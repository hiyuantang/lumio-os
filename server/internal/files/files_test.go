// SPDX-License-Identifier: AGPL-3.0-only
package files

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestList(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("hello.txt", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	res, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if res.Path != filepath.Clean(dir) {
		t.Errorf("path = %q", res.Path)
	}
	if len(res.Entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(res.Entries))
	}
	if res.Entries[0].Name != "hello.txt" || res.Entries[1].Name != "link" || res.Entries[2].Name != "sub" {
		t.Errorf("entries not sorted: %v", res.Entries)
	}
	if res.Entries[0].Type != "file" || res.Entries[0].Mode != "0644" || res.Entries[0].SizeBytes != 5 {
		t.Errorf("file entry = %+v", res.Entries[0])
	}
	if res.Entries[1].Type != "symlink" || res.Entries[1].SymlinkTarget == nil || *res.Entries[1].SymlinkTarget != "hello.txt" {
		t.Errorf("symlink entry = %+v", res.Entries[1])
	}
	if res.Entries[2].Type != "directory" {
		t.Errorf("dir entry = %+v", res.Entries[2])
	}
}

func TestListValidation(t *testing.T) {
	if _, err := List("relative/path"); !errors.Is(err, ErrValidation) {
		t.Errorf("relative path: %v", err)
	}
	if _, err := List(""); !errors.Is(err, ErrValidation) {
		t.Errorf("empty path: %v", err)
	}
	if _, err := List("/definitely/not/here"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing path: %v", err)
	}
}

func TestRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	content := []byte("hello world")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}
	res, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if res.Encoding != "utf-8" || res.Content == nil {
		t.Fatalf("res = %+v", res)
	}
	decoded, err := base64.StdEncoding.DecodeString(*res.Content)
	if err != nil || !bytes.Equal(decoded, content) {
		t.Errorf("content mismatch: %v %q", err, decoded)
	}
	sum := sha256.Sum256(content)
	if res.Revision != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Errorf("revision = %q", res.Revision)
	}
	if res.Truncated || res.SizeBytes != int64(len(content)) {
		t.Errorf("res = %+v", res)
	}
}

func TestReadBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blob.bin")
	if err := os.WriteFile(path, []byte{0x89, 0x50, 0x4E, 0x47, 0x00, 0x01}, 0644); err != nil {
		t.Fatal(err)
	}
	res, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if res.Encoding != "binary" || res.Content != nil {
		t.Errorf("binary file should be flagged, not decoded: %+v", res)
	}
	if res.Revision == "" {
		t.Error("binary files still carry a revision")
	}
}

func TestReadTruncated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	content := bytes.Repeat([]byte("a"), MaxReadBytes+100)
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}
	res, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !res.Truncated {
		t.Error("expected truncated=true")
	}
	if res.SizeBytes != int64(len(content)) {
		t.Errorf("sizeBytes = %d", res.SizeBytes)
	}
	decoded, err := base64.StdEncoding.DecodeString(*res.Content)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != MaxReadBytes {
		t.Errorf("decoded length = %d", len(decoded))
	}
	sum := sha256.Sum256(content)
	if res.Revision != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Error("revision must hash the full file, not the truncated view")
	}
}

func TestReadValidation(t *testing.T) {
	if _, err := Read("tmp/x"); !errors.Is(err, ErrValidation) {
		t.Errorf("relative path: %v", err)
	}
	if _, err := Read(t.TempDir()); !errors.Is(err, ErrValidation) {
		t.Errorf("directory read: %v", err)
	}
	if _, err := Read("/definitely/not/here"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing path: %v", err)
	}
}

func TestListForbidden(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(locked, 0755)
	_, err := List(locked)
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("expected permission error, got %v", err)
	}
}

func TestDetectEncoding(t *testing.T) {
	if detectEncoding([]byte("plain text")) != "utf-8" {
		t.Error("text should be utf-8")
	}
	if detectEncoding([]byte{'a', 0x00, 'b'}) != "binary" {
		t.Error("NUL byte means binary")
	}
	if detectEncoding([]byte{0xff, 0xfe, 0xfd}) != "binary" {
		t.Error("invalid UTF-8 means binary")
	}
	truncated := strings.Repeat("x", detectByteCount-1) + "é"
	if detectEncoding([]byte(truncated)) != "utf-8" {
		t.Error("trailing partial rune should still be utf-8")
	}
}
