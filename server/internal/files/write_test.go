// SPDX-License-Identifier: AGPL-3.0-only
package files

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")
	res, err := Write(path, []byte("v1-content"), "")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if res.SizeBytes != 10 {
		t.Errorf("sizeBytes = %d", res.SizeBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, []byte("v1-content")) {
		t.Fatalf("content mismatch: %v %q", err, data)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o644 {
		t.Errorf("mode = %o", info.Mode().Perm())
	}
	readRes, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if readRes.Revision != res.Revision {
		t.Errorf("write revision %q != read revision %q", res.Revision, readRes.Revision)
	}
}

func TestWritePreservesModeAndRevisionFlow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Write(path, []byte("new-content"), first.Revision)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if res.Revision == first.Revision {
		t.Error("revision should change")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 600", info.Mode().Perm())
	}
	data, _ := os.ReadFile(path)
	if !bytes.Equal(data, []byte("new-content")) {
		t.Errorf("content = %q", data)
	}
}

func TestWriteStaleRevision(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stale.txt")
	if err := os.WriteFile(path, []byte("on-disk"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Write(path, []byte("client-view"), "sha256:deadbeef")
	var stale *StaleError
	if !errors.As(err, &stale) {
		t.Fatalf("expected StaleError, got %v", err)
	}
	if stale.Expected != "sha256:deadbeef" || !errors.Is(err, ErrStaleRevision) {
		t.Errorf("stale = %+v", stale)
	}
	data, _ := os.ReadFile(path)
	if !bytes.Equal(data, []byte("on-disk")) {
		t.Error("stale write must not touch the file")
	}
}

func TestWriteExpectedOnMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := Write(filepath.Join(dir, "missing.txt"), []byte("x"), "sha256:deadbeef")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("expected not-exist, got %v", err)
	}
}

func TestWriteValidation(t *testing.T) {
	dir := t.TempDir()
	if _, err := Write("relative.txt", []byte("x"), ""); !errors.Is(err, ErrValidation) {
		t.Errorf("relative: %v", err)
	}
	if _, err := Write(dir, []byte("x"), ""); !errors.Is(err, ErrValidation) {
		t.Errorf("directory: %v", err)
	}
	if _, err := Write(filepath.Join(dir, "nope", "x.txt"), []byte("x"), ""); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing parent: %v", err)
	}
}

func TestWriteConcurrentSamePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "race.txt")
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func(b byte) {
			_, err := Write(path, bytes.Repeat([]byte{b}, 4096), "")
			done <- err
		}(byte('a' + i))
	}
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatalf("concurrent write: %v", err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 4096 || (data[0] != 'a' && data[0] != 'b') {
		t.Errorf("corrupted content, len=%d first=%q", len(data), data[0])
	}
	for _, b := range data {
		if b != data[0] {
			t.Fatal("interleaved writes detected")
		}
	}
}
