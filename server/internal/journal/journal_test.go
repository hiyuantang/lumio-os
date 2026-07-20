// SPDX-License-Identifier: AGPL-3.0-only
package journal

import (
	"errors"
	"strings"
	"testing"
)

func TestQueryValidate(t *testing.T) {
	valid := []Query{
		{},
		{Unit: "nginx.service", Priority: "warning", Since: "2026-07-19T00:10:02Z", Boot: "current", Limit: 50, After: "s=abc;i=1"},
		{Boot: "previous"},
		{Priority: "4"},
		{Unit: "my-unit@instance.service"},
	}
	for _, q := range valid {
		if err := q.Validate(); err != nil {
			t.Errorf("query %+v should be valid: %v", q, err)
		}
	}
	invalid := []Query{
		{Unit: "-u"},
		{Unit: "bad unit"},
		{Unit: "a;rm -rf"},
		{Priority: "verbose"},
		{Since: "yesterday"},
		{Boot: "old"},
		{Limit: -1},
		{Limit: MaxLimit + 1},
		{After: "-x"},
	}
	for _, q := range invalid {
		if err := q.Validate(); !errors.Is(err, ErrValidation) {
			t.Errorf("query %+v should fail validation, got %v", q, err)
		}
	}
}

func TestCLIArgs(t *testing.T) {
	cli := &CLI{bin: "/usr/bin/journalctl"}
	q := Query{Unit: "cron.service", Priority: "Warning", Since: "2026-07-19T00:10:02Z", Boot: "previous", Limit: 25, After: "s=abc;i=1"}
	args := strings.Join(cli.args(q, false), " ")
	for _, want := range []string{
		"--output=json",
		"-u cron.service",
		"-p warning",
		"--since 2026-07-19 00:10:02 UTC",
		"--boot=-1",
		"--after-cursor s=abc;i=1",
		"-n 25",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("args %q missing %q", args, want)
		}
	}

	follow := strings.Join(cli.args(Query{}, true), " ")
	if !strings.Contains(follow, "-f -n 0") {
		t.Errorf("follow args %q missing -f -n 0", follow)
	}

	def := strings.Join(cli.args(Query{}, false), " ")
	if !strings.Contains(def, "-n 200") {
		t.Errorf("default limit args %q missing -n 200", def)
	}
}

func TestFormatSince(t *testing.T) {
	if got := formatSince("2026-07-19T05:30:00+03:00"); got != "2026-07-19 02:30:00 UTC" {
		t.Errorf("formatSince = %q", got)
	}
}

func TestPreviousBootUnavailable(t *testing.T) {
	if !previousBootUnavailable("Data from the specified boot (-1) is not available") {
		t.Fatal("expected missing previous boot to be recognised")
	}
	if previousBootUnavailable("permission denied") {
		t.Fatal("unrelated errors must not be hidden")
	}
}
