// SPDX-License-Identifier: AGPL-3.0-only
package updates

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	outputs map[string]string
	err     error
}

func commandKey(name string, args ...string) string {
	return name + " " + strings.Join(args, " ")
}

func (f fakeRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	return []byte(f.outputs[commandKey(name, args...)]), f.err
}

func (f fakeRunner) Stream(_ context.Context, _ string, _ []string, _ func(string)) error {
	return f.err
}

func TestCalculatePlanParsesSecurityAndSizes(t *testing.T) {
	runner := fakeRunner{outputs: map[string]string{}}
	runner.outputs[commandKey("apt-get", "-s", "-V", "-o", "Dpkg::Use-Pty=0", "upgrade")] = strings.Join([]string{
		"Inst openssl [3.0.1] (3.0.2 Ubuntu:24.04/noble-security [amd64])",
		"Inst systemd [255.1] (255.2 Ubuntu:24.04/noble-updates [amd64])",
	}, "\n")
	runner.outputs[commandKey("apt-cache", "show", "--no-all-versions", "openssl=3.0.2")] = "Size: 1000\nInstalled-Size: 12\n"
	runner.outputs[commandKey("apt-cache", "show", "--no-all-versions", "systemd=255.2")] = "Size: 2000\nInstalled-Size: 22\n"
	runner.outputs[commandKey("dpkg-query", "-W", "-f=${Installed-Size}", "openssl")] = "10"
	runner.outputs[commandKey("dpkg-query", "-W", "-f=${Installed-Size}", "systemd")] = "20"
	worker := &Worker{
		runner: runner, aptGet: "apt-get", aptCache: "apt-cache", dpkg: "dpkg-query",
		plans: map[string]Plan{}, progress: map[string]Progress{},
	}
	plan, err := worker.CalculatePlan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Packages) != 2 || plan.SecurityCount != 1 {
		t.Fatalf("plan=%+v", plan)
	}
	if plan.DownloadBytes != 3000 || plan.InstalledDeltaBytes != 4096 {
		t.Fatalf("sizes download=%d delta=%d", plan.DownloadBytes, plan.InstalledDeltaBytes)
	}
	if !strings.HasPrefix(plan.ID, "pln_") || len(plan.ID) != 28 {
		t.Errorf("plan id=%q", plan.ID)
	}
}

func TestStartApplyRequiresMatchingLivePlan(t *testing.T) {
	now := time.Now().UTC()
	worker := &Worker{
		runner: fakeRunner{}, aptGet: "apt-get", aptCache: "apt-cache", dpkg: "dpkg-query",
		plans: map[string]Plan{
			"pln_000000000000000000000000": {
				ID: "pln_000000000000000000000000", CreatedAt: now, ExpiresAt: now.Add(time.Minute), Packages: []Package{},
			},
		},
		progress: map[string]Progress{},
	}
	if _, _, err := worker.StartApply("pln_000000000000000000000000", "pln_wrong", "request-1", func(Progress) {}); !errors.Is(err, ErrPlanStale) {
		t.Fatalf("mismatch err=%v", err)
	}
	done := make(chan Progress, 1)
	initial, replay, err := worker.StartApply(
		"pln_000000000000000000000000",
		"pln_000000000000000000000000",
		"request-2",
		func(progress Progress) { done <- progress },
	)
	if err != nil || replay || initial.Phase != "queued" {
		t.Fatalf("initial=%+v replay=%v err=%v", initial, replay, err)
	}
	select {
	case progress := <-done:
		if !progress.Done || !progress.Success || progress.Percent != 100 {
			t.Fatalf("progress=%+v", progress)
		}
	case <-time.After(time.Second):
		t.Fatal("apply did not finish")
	}
}
