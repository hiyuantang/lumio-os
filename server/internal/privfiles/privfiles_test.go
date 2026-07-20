// SPDX-License-Identifier: AGPL-3.0-only
package privfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testWriter(t *testing.T) (*Writer, string, string) {
	t.Helper()
	root := t.TempDir()
	rollback := t.TempDir()
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	root = realRoot
	return &Writer{allowedRoot: root, rollbackDir: rollback}, root, rollback
}

func TestWriteCreatesRollbackAndPreservesMode(t *testing.T) {
	writer, root, rollback := testWriter(t)
	path := filepath.Join(root, "app.conf")
	old := []byte("enabled=false\n")
	if err := os.WriteFile(path, old, 0o640); err != nil {
		t.Fatal(err)
	}
	result, err := writer.Write(context.Background(), path, []byte("enabled=true\n"), revision(old), "", "req-1")
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "enabled=true\n" {
		t.Fatalf("content=%q err=%v", content, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode=%v", info.Mode().Perm())
	}
	rollbackContent, err := os.ReadFile(filepath.Join(rollback, result.RollbackRef))
	if err != nil || string(rollbackContent) != string(old) {
		t.Fatalf("rollback=%q err=%v", rollbackContent, err)
	}
	if result.Revision != revision([]byte("enabled=true\n")) {
		t.Errorf("revision=%q", result.Revision)
	}
}

func TestWriteRejectsStaleRevisionAndSymlink(t *testing.T) {
	writer, root, _ := testWriter(t)
	path := filepath.Join(root, "app.conf")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(context.Background(), path, []byte("new"), "sha256:"+strings.Repeat("0", 64), "", "req-2"); !errors.Is(err, ErrStale) {
		t.Fatalf("stale err=%v", err)
	}
	link := filepath.Join(root, "link.conf")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(context.Background(), link, []byte("new"), revision([]byte("old")), "", "req-3"); !errors.Is(err, ErrValidation) {
		t.Fatalf("symlink err=%v", err)
	}
}

func TestWriteValidatesJSONBeforeMutation(t *testing.T) {
	writer, root, rollback := testWriter(t)
	path := filepath.Join(root, "settings.json")
	old := []byte("{\"enabled\":false}\n")
	if err := os.WriteFile(path, old, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(context.Background(), path, []byte("{"), revision(old), "", "req-4"); !errors.Is(err, ErrValidation) {
		t.Fatalf("validation err=%v", err)
	}
	content, _ := os.ReadFile(path)
	if string(content) != string(old) {
		t.Fatalf("file mutated to %q", content)
	}
	entries, _ := os.ReadDir(rollback)
	if len(entries) != 0 {
		t.Fatalf("rollback created before validation: %v", entries)
	}
}
