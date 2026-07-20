// SPDX-License-Identifier: AGPL-3.0-only
package updates

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const planLifetime = 15 * time.Minute

var (
	ErrUnavailable = errors.New("package manager unavailable")
	ErrBusy        = errors.New("package manager busy")
	ErrPlanMissing = errors.New("update plan not found")
	ErrPlanStale   = errors.New("update plan expired")
)

type Package struct {
	Name                string `json:"name"`
	FromVersion         string `json:"fromVersion"`
	ToVersion           string `json:"toVersion"`
	Security            bool   `json:"security"`
	DownloadBytes       int64  `json:"downloadBytes"`
	InstalledDeltaBytes int64  `json:"installedDeltaBytes"`
}

type Plan struct {
	ID                  string    `json:"id"`
	CreatedAt           time.Time `json:"createdAt"`
	ExpiresAt           time.Time `json:"expiresAt"`
	Packages            []Package `json:"packages"`
	SecurityCount       int       `json:"securityCount"`
	DownloadBytes       int64     `json:"downloadBytes"`
	InstalledDeltaBytes int64     `json:"installedDeltaBytes"`
	RebootRequired      bool      `json:"rebootRequired"`
}

type RefreshResult struct {
	RefreshedAt time.Time `json:"refreshedAt"`
}

type Progress struct {
	RequestID string    `json:"requestId"`
	PlanID    string    `json:"planId"`
	Phase     string    `json:"phase"`
	Percent   int       `json:"percent"`
	Message   string    `json:"message"`
	Done      bool      `json:"done"`
	Success   bool      `json:"success"`
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type commandRunner interface {
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
	Stream(ctx context.Context, name string, args []string, onLine func(string)) error
}

type execRunner struct{}

func (execRunner) command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "DEBIAN_FRONTEND=noninteractive")
	return cmd
}

func (r execRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return r.command(ctx, name, args...).CombinedOutput()
}

func (r execRunner) Stream(ctx context.Context, name string, args []string, onLine func(string)) error {
	cmd := r.command(ctx, name, args...)
	reader, writer := io.Pipe()
	cmd.Stdout = writer
	cmd.Stderr = writer
	if err := cmd.Start(); err != nil {
		_ = writer.Close()
		_ = reader.Close()
		return err
	}
	wait := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		_ = writer.Close()
		wait <- err
	}()
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		onLine(scanner.Text())
	}
	_ = reader.Close()
	if err := <-wait; err != nil {
		return err
	}
	return scanner.Err()
}

type Worker struct {
	runner   commandRunner
	aptGet   string
	aptCache string
	dpkg     string

	opMu sync.Mutex
	mu   sync.Mutex

	plans    map[string]Plan
	progress map[string]Progress
}

func NewWorker() *Worker {
	aptGet, _ := exec.LookPath("apt-get")
	aptCache, _ := exec.LookPath("apt-cache")
	dpkg, _ := exec.LookPath("dpkg-query")
	return &Worker{
		runner:   execRunner{},
		aptGet:   aptGet,
		aptCache: aptCache,
		dpkg:     dpkg,
		plans:    map[string]Plan{},
		progress: map[string]Progress{},
	}
}

func (w *Worker) Available() bool {
	return w != nil && w.aptGet != "" && w.aptCache != "" && w.dpkg != ""
}

func (w *Worker) Refresh(ctx context.Context) (RefreshResult, error) {
	if !w.Available() {
		return RefreshResult{}, ErrUnavailable
	}
	if !w.opMu.TryLock() {
		return RefreshResult{}, ErrBusy
	}
	defer w.opMu.Unlock()
	output, err := w.runner.Output(ctx, w.aptGet, "-o", "Dpkg::Use-Pty=0", "update")
	if err != nil {
		return RefreshResult{}, fmt.Errorf("apt metadata refresh failed: %s", commandFailure(output, err))
	}
	return RefreshResult{RefreshedAt: time.Now().UTC()}, nil
}

var planLinePattern = regexp.MustCompile(`^Inst\s+(\S+)(?:\s+\[([^\]]+)\])?\s+\((\S+)(?:\s+([^)]*))?\)`)

func (w *Worker) CalculatePlan(ctx context.Context) (Plan, error) {
	if !w.Available() {
		return Plan{}, ErrUnavailable
	}
	if !w.opMu.TryLock() {
		return Plan{}, ErrBusy
	}
	defer w.opMu.Unlock()
	output, err := w.runner.Output(ctx, w.aptGet, "-s", "-V", "-o", "Dpkg::Use-Pty=0", "upgrade")
	if err != nil {
		return Plan{}, fmt.Errorf("apt plan failed: %s", commandFailure(output, err))
	}
	packages := parsePlanOutput(string(output))
	for i := range packages {
		packages[i].DownloadBytes, packages[i].InstalledDeltaBytes = w.packageSizes(ctx, packages[i])
	}
	now := time.Now().UTC()
	plan := Plan{
		ID:             "pln_" + randomID(),
		CreatedAt:      now,
		ExpiresAt:      now.Add(planLifetime),
		Packages:       packages,
		RebootRequired: fileExists("/var/run/reboot-required"),
	}
	for _, pkg := range packages {
		if pkg.Security {
			plan.SecurityCount++
		}
		plan.DownloadBytes += pkg.DownloadBytes
		plan.InstalledDeltaBytes += pkg.InstalledDeltaBytes
	}
	w.mu.Lock()
	w.pruneLocked(now)
	w.plans[plan.ID] = plan
	w.mu.Unlock()
	return plan, nil
}

func parsePlanOutput(output string) []Package {
	packages := []Package{}
	for _, line := range strings.Split(output, "\n") {
		match := planLinePattern.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		packages = append(packages, Package{
			Name:        match[1],
			FromVersion: match[2],
			ToVersion:   match[3],
			Security:    strings.Contains(strings.ToLower(match[4]), "-security"),
		})
	}
	return packages
}

func (w *Worker) packageSizes(ctx context.Context, pkg Package) (int64, int64) {
	var download, installedNew, installedOld int64
	if output, err := w.runner.Output(ctx, w.aptCache, "show", "--no-all-versions", pkg.Name+"="+pkg.ToVersion); err == nil {
		for _, line := range strings.Split(string(output), "\n") {
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			switch key {
			case "Size":
				download, _ = strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			case "Installed-Size":
				kilobytes, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
				installedNew = kilobytes * 1024
			}
		}
	}
	if output, err := w.runner.Output(ctx, w.dpkg, "-W", "-f=${Installed-Size}", pkg.Name); err == nil {
		kilobytes, _ := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
		installedOld = kilobytes * 1024
	}
	return download, installedNew - installedOld
}

func (w *Worker) StartApply(planID, expectedPlanID, requestID string, done func(Progress)) (Progress, bool, error) {
	if !w.Available() {
		return Progress{}, false, ErrUnavailable
	}
	now := time.Now().UTC()
	w.mu.Lock()
	if existing, ok := w.progress[requestID]; ok {
		w.mu.Unlock()
		return existing, true, nil
	}
	plan, ok := w.plans[planID]
	if !ok {
		w.mu.Unlock()
		return Progress{}, false, ErrPlanMissing
	}
	if expectedPlanID == "" || expectedPlanID != planID {
		w.mu.Unlock()
		return Progress{}, false, ErrPlanStale
	}
	if !plan.ExpiresAt.After(now) {
		delete(w.plans, planID)
		w.mu.Unlock()
		return Progress{}, false, ErrPlanStale
	}
	progress := Progress{
		RequestID: requestID,
		PlanID:    planID,
		Phase:     "queued",
		Percent:   0,
		Message:   "Waiting for the package manager",
		UpdatedAt: now,
	}
	w.progress[requestID] = progress
	w.mu.Unlock()
	go w.apply(plan, requestID, done)
	return progress, false, nil
}

func (w *Worker) apply(plan Plan, requestID string, done func(Progress)) {
	w.opMu.Lock()
	defer w.opMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	if len(plan.Packages) == 0 {
		progress := w.finish(requestID, true, "")
		done(progress)
		return
	}
	targets := make([]string, 0, len(plan.Packages))
	for _, pkg := range plan.Packages {
		targets = append(targets, pkg.Name+"="+pkg.ToVersion)
	}
	args := []string{"-y", "--no-remove", "-o", "Dpkg::Use-Pty=0", "install", "--"}
	args = append(args, targets...)
	w.setProgress(requestID, "downloading", 2, "Downloading packages")
	configured := 0
	lastLine := ""
	err := w.runner.Stream(ctx, w.aptGet, args, func(line string) {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			lastLine = trimmed
		}
		switch {
		case strings.HasPrefix(trimmed, "Get:"):
			w.setProgress(requestID, "downloading", 10, trimmed)
		case strings.HasPrefix(trimmed, "Unpacking "):
			w.setProgress(requestID, "installing", 45, trimmed)
		case strings.HasPrefix(trimmed, "Setting up "):
			configured++
			percent := 50 + (configured*45)/len(plan.Packages)
			w.setProgress(requestID, "installing", min(percent, 95), trimmed)
		}
	})
	if err != nil {
		errorText := err.Error()
		if lastLine != "" {
			errorText += ": " + lastLine
		}
		progress := w.finish(requestID, false, errorText)
		done(progress)
		return
	}
	progress := w.finish(requestID, true, "")
	done(progress)
}

func (w *Worker) Progress(requestID string) (Progress, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	progress, ok := w.progress[requestID]
	return progress, ok
}

func (w *Worker) setProgress(requestID, phase string, percent int, message string) Progress {
	w.mu.Lock()
	defer w.mu.Unlock()
	progress := w.progress[requestID]
	progress.Phase = phase
	progress.Percent = percent
	progress.Message = message
	progress.UpdatedAt = time.Now().UTC()
	w.progress[requestID] = progress
	return progress
}

func (w *Worker) finish(requestID string, success bool, errorText string) Progress {
	w.mu.Lock()
	defer w.mu.Unlock()
	progress := w.progress[requestID]
	progress.Phase = "complete"
	if success {
		progress.Percent = 100
	}
	progress.Done = true
	progress.Success = success
	progress.Error = errorText
	if success {
		progress.Message = "Updates installed"
	} else if errorText == ErrBusy.Error() {
		progress.Message = "Package manager busy"
	} else {
		progress.Message = "Update installation failed"
	}
	progress.UpdatedAt = time.Now().UTC()
	w.progress[requestID] = progress
	return progress
}

func (w *Worker) pruneLocked(now time.Time) {
	for id, plan := range w.plans {
		if !plan.ExpiresAt.After(now) {
			delete(w.plans, id)
		}
	}
	for id, progress := range w.progress {
		if progress.Done && now.Sub(progress.UpdatedAt) > time.Hour {
			delete(w.progress, id)
		}
	}
}

func randomID() string {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(value[:])
}

func commandFailure(output []byte, err error) string {
	text := strings.TrimSpace(string(output))
	if len(text) > 800 {
		text = text[len(text)-800:]
	}
	if text == "" {
		return err.Error()
	}
	return text
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
