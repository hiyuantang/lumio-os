// SPDX-License-Identifier: AGPL-3.0-only
package files

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTrashHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	return home
}

func TestTrashFile(t *testing.T) {
	home := setupTrashHome(t)
	path := filepath.Join(home, "doomed.txt")
	if err := os.WriteFile(path, []byte("bye"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Trash(path)
	if err != nil {
		t.Fatalf("Trash: %v", err)
	}
	if !res.Trashed {
		t.Error("trashed should be true")
	}
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Error("original should be gone")
	}
	trashed := filepath.Join(home, ".local", "share", "Trash", "files", "doomed.txt")
	data, err := os.ReadFile(trashed)
	if err != nil || string(data) != "bye" {
		t.Errorf("trash copy: %v %q", err, data)
	}
	info, err := os.ReadFile(filepath.Join(home, ".local", "share", "Trash", "info", "doomed.txt.trashinfo"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(info), "Path="+resolved) || !strings.Contains(string(info), "DeletionDate=") {
		t.Errorf("trashinfo = %q", info)
	}
}

func TestTrashNameCollision(t *testing.T) {
	home := setupTrashHome(t)
	for _, content := range []string{"one", "two"} {
		path := filepath.Join(home, "same.txt")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Trash(path); err != nil {
			t.Fatalf("Trash %q: %v", content, err)
		}
	}
	filesDir := filepath.Join(home, ".local", "share", "Trash", "files")
	for _, want := range []string{"same.txt", "same.txt.1"} {
		if _, err := os.Lstat(filepath.Join(filesDir, want)); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}
}

func TestTrashValidation(t *testing.T) {
	home := setupTrashHome(t)
	if _, err := Trash("relative.txt"); !errors.Is(err, ErrValidation) {
		t.Errorf("relative: %v", err)
	}
	if _, err := Trash("/"); !errors.Is(err, ErrValidation) {
		t.Errorf("root: %v", err)
	}
	if _, err := Trash(filepath.Join(home, "missing.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing: %v", err)
	}
	inside := filepath.Join(home, ".local", "share", "Trash", "files")
	if err := os.MkdirAll(inside, 0o700); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(inside, "already.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Trash(f); !errors.Is(err, ErrValidation) {
		t.Errorf("inside trash: %v", err)
	}
}

func TestTrashUnreadableTarget(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	setupTrashHome(t)
	if _, err := Trash("/etc/sudoers"); err == nil {
		t.Skip("host allows renaming /etc/sudoers")
	} else if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("expected permission error, got %v", err)
	}
}
