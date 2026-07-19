// SPDX-License-Identifier: AGPL-3.0-only
package journal

import (
	"testing"
	"time"
)

func TestParseLine(t *testing.T) {
	line := []byte(`{"__CURSOR":"s=abc;i=41f;b=1e4c9a;m=3f2a1;t=5f8c;e=101","__REALTIME_TIMESTAMP":"1721400002113000","PRIORITY":"4","_SYSTEMD_UNIT":"nginx.service","MESSAGE":"upstream timed out","_PID":"812","_COMM":"nginx","__MONOTONIC_TIMESTAMP":"123"}`)
	entry, err := parseLine(line)
	if err != nil {
		t.Fatalf("parseLine: %v", err)
	}
	if entry.Cursor != "s=abc;i=41f;b=1e4c9a;m=3f2a1;t=5f8c;e=101" {
		t.Errorf("cursor = %q", entry.Cursor)
	}
	wantTS := time.UnixMicro(1721400002113000).UTC().Format("2006-01-02T15:04:05.999Z07:00")
	if entry.TS != wantTS {
		t.Errorf("ts = %q, want %q", entry.TS, wantTS)
	}
	if entry.Priority != "warning" {
		t.Errorf("priority = %q", entry.Priority)
	}
	if entry.Unit != "nginx.service" {
		t.Errorf("unit = %q", entry.Unit)
	}
	if entry.Message != "upstream timed out" {
		t.Errorf("message = %q", entry.Message)
	}
	if entry.Fields["_PID"] != "812" || entry.Fields["_COMM"] != "nginx" {
		t.Errorf("fields = %v", entry.Fields)
	}
	if _, ok := entry.Fields["__MONOTONIC_TIMESTAMP"]; ok {
		t.Error("double-underscore fields must be skipped")
	}
}

func TestParseLineArrayMessage(t *testing.T) {
	line := []byte(`{"__CURSOR":"s=x;i=1","__REALTIME_TIMESTAMP":"1721400002000000","PRIORITY":"6","_SYSTEMD_UNIT":"cron.service","MESSAGE":[104,101,108,108,111,32,119,111,114,108,100]}`)
	entry, err := parseLine(line)
	if err != nil {
		t.Fatalf("parseLine: %v", err)
	}
	if entry.Message != "hello world" {
		t.Errorf("message = %q", entry.Message)
	}
	if entry.Priority != "info" {
		t.Errorf("priority = %q", entry.Priority)
	}
}

func TestParseLineErrors(t *testing.T) {
	if _, err := parseLine([]byte(`not json`)); err == nil {
		t.Error("expected error for invalid JSON")
	}
	if _, err := parseLine([]byte(`{"MESSAGE":"no cursor"}`)); err == nil {
		t.Error("expected error for missing cursor")
	}
	if _, err := parseLine(nil); err == nil {
		t.Error("expected error for empty line")
	}
}

func TestPriorityName(t *testing.T) {
	if priorityName("3") != "err" {
		t.Error("3 should map to err")
	}
	if priorityName("warning") != "warning" {
		t.Error("names pass through")
	}
}
