// SPDX-License-Identifier: AGPL-3.0-only
package files

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

const MaxWriteBytes = 8 << 20

var ErrStaleRevision = errors.New("stale revision")

type StaleError struct {
	Expected string
	Actual   string
}

func (e *StaleError) Error() string {
	return "the file changed on disk since it was read"
}

func (e *StaleError) Unwrap() error { return ErrStaleRevision }

type WriteResult struct {
	Path      string `json:"path"`
	Revision  string `json:"revision"`
	SizeBytes int64  `json:"sizeBytes"`
}

var writeLocks = &lockTable{locks: map[string]*sync.Mutex{}}

type lockTable struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func (t *lockTable) lock(key string) func() {
	t.mu.Lock()
	l, ok := t.locks[key]
	if !ok {
		l = &sync.Mutex{}
		t.locks[key] = l
	}
	t.mu.Unlock()
	l.Lock()
	return l.Unlock
}

func Write(p string, content []byte, expectedRevision string) (WriteResult, error) {
	clean, err := cleanPath(p)
	if err != nil {
		return WriteResult{}, err
	}
	dir := filepath.Dir(clean)
	name := filepath.Base(clean)
	if name == "" || name == "." || name == ".." {
		return WriteResult{}, fmt.Errorf("%w: invalid file name", ErrValidation)
	}
	realDir, err := resolve(dir)
	if err != nil {
		return WriteResult{}, err
	}
	target := filepath.Join(realDir, name)

	unlock := writeLocks.lock(target)
	defer unlock()

	var existing os.FileInfo
	info, statErr := os.Stat(target)
	switch {
	case statErr == nil:
		if info.IsDir() {
			return WriteResult{}, fmt.Errorf("%w: path is a directory", ErrValidation)
		}
		existing = info
	case errors.Is(statErr, fs.ErrNotExist):
	default:
		return WriteResult{}, statErr
	}

	if expectedRevision != "" {
		if existing == nil {
			return WriteResult{}, &os.PathError{Op: "open", Path: target, Err: fs.ErrNotExist}
		}
		actual, err := hashFile(target)
		if err != nil {
			return WriteResult{}, err
		}
		if actual != expectedRevision {
			return WriteResult{}, &StaleError{Expected: expectedRevision, Actual: actual}
		}
	}

	tmp, err := os.CreateTemp(realDir, ".lumio-write-*")
	if err != nil {
		return WriteResult{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	hasher := sha256.New()
	if _, err := io.Copy(tmp, io.TeeReader(bytes.NewReader(content), hasher)); err != nil {
		_ = tmp.Close()
		return WriteResult{}, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return WriteResult{}, err
	}
	if existing != nil {
		if err := tmp.Chmod(existing.Mode().Perm()); err != nil {
			_ = tmp.Close()
			return WriteResult{}, err
		}
		uid, gid := ownership(existing)
		if err := os.Chown(tmpName, uid, gid); err != nil {
			_ = tmp.Close()
			return WriteResult{}, err
		}
	} else {
		if err := tmp.Chmod(0o644); err != nil {
			_ = tmp.Close()
			return WriteResult{}, err
		}
	}
	if err := tmp.Close(); err != nil {
		return WriteResult{}, err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return WriteResult{}, err
	}
	syncDir(realDir)
	return WriteResult{
		Path:      clean,
		Revision:  "sha256:" + hex.EncodeToString(hasher.Sum(nil)),
		SizeBytes: int64(len(content)),
	}, nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil)), nil
}

func ownership(info os.FileInfo) (uid, gid int) {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return int(st.Uid), int(st.Gid)
	}
	return -1, -1
}

func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}
