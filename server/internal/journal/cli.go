// SPDX-License-Identifier: AGPL-3.0-only
package journal

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const scanBufSize = 4 * 1024 * 1024

type CLI struct {
	bin string
}

func NewCLI() *CLI {
	bin, err := exec.LookPath("journalctl")
	if err != nil {
		return &CLI{}
	}
	return &CLI{bin: bin}
}

func (c *CLI) Available() bool {
	return c != nil && c.bin != ""
}

func (c *CLI) Query(ctx context.Context, q Query) (Result, error) {
	if !c.Available() {
		return Result{}, ErrUnavailable
	}
	if err := q.Validate(); err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.bin, c.args(q, false)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if q.Boot == "previous" && previousBootUnavailable(stderr.String()) {
			return Result{Entries: []Entry{}}, nil
		}
		return Result{}, cliError(err, stderr.String())
	}
	res := Result{Entries: []Entry{}}
	sc := bufio.NewScanner(&stdout)
	sc.Buffer(make([]byte, 64*1024), scanBufSize)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		entry, err := parseLine(line)
		if err != nil {
			continue
		}
		res.Entries = append(res.Entries, entry)
	}
	if err := sc.Err(); err != nil {
		return Result{}, fmt.Errorf("reading journalctl output: %w", err)
	}
	if n := len(res.Entries); n > 0 {
		res.NextCursor = res.Entries[n-1].Cursor
	}
	return res, nil
}

func previousBootUnavailable(stderr string) bool {
	return strings.Contains(stderr, "specified boot") || strings.Contains(stderr, "No such boot ID")
}

func (c *CLI) Follow(ctx context.Context, q Query, emit func(Entry) bool) error {
	if !c.Available() {
		return ErrUnavailable
	}
	if err := q.Validate(); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, c.bin, c.args(q, true)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("journalctl stdout: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting journalctl: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), scanBufSize)
	keepGoing := true
	for keepGoing && sc.Scan() {
		entry, err := parseLine(bytes.TrimSpace(sc.Bytes()))
		if err != nil {
			continue
		}
		keepGoing = emit(entry)
	}
	if !keepGoing || ctx.Err() != nil {
		return nil
	}
	select {
	case err := <-done:
		if err != nil {
			return cliError(err, stderr.String())
		}
		return fmt.Errorf("journalctl exited while following")
	case <-time.After(2 * time.Second):
		return fmt.Errorf("journalctl follow ended abnormally")
	}
}

func (c *CLI) args(q Query, follow bool) []string {
	args := []string{"--output=json", "--no-pager", "--quiet"}
	if q.Unit != "" {
		args = append(args, "-u", q.Unit)
	}
	if q.Priority != "" {
		args = append(args, "-p", strings.ToLower(q.Priority))
	}
	if q.Since != "" {
		args = append(args, "--since", formatSince(q.Since))
	}
	if q.Boot == "current" {
		args = append(args, "--boot=0")
	} else if q.Boot == "previous" {
		args = append(args, "--boot=-1")
	}
	if q.After != "" {
		args = append(args, "--after-cursor", q.After)
	}
	if follow {
		args = append(args, "-f", "-n", "0")
	} else {
		args = append(args, "-n", strconv.Itoa(q.limit()))
	}
	return args
}

func formatSince(since string) string {
	t, err := time.Parse(time.RFC3339, since)
	if err != nil {
		return since
	}
	return t.UTC().Format("2006-01-02 15:04:05") + " UTC"
}

func cliError(err error, stderr string) error {
	stderr = strings.TrimSpace(stderr)
	if strings.Contains(stderr, "cursor") {
		return fmt.Errorf("%w: %s", ErrValidation, stderr)
	}
	if stderr != "" {
		return fmt.Errorf("journalctl failed: %v: %s", err, stderr)
	}
	return fmt.Errorf("journalctl failed: %v", err)
}
