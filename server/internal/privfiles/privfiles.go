// SPDX-License-Identifier: AGPL-3.0-only
package privfiles

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const MaxWriteBytes = 1 << 20

var (
	ErrValidation = errors.New("invalid privileged file request")
	ErrStale      = errors.New("stale privileged file revision")
)

type StaleError struct {
	Expected string
	Actual   string
}

func (e *StaleError) Error() string {
	return "the protected file changed on disk since it was read"
}

func (e *StaleError) Unwrap() error { return ErrStale }

type Validation struct {
	Kind    string `json:"kind"`
	Checked bool   `json:"checked"`
}

type Result struct {
	Path        string     `json:"path"`
	Revision    string     `json:"revision"`
	SizeBytes   int64      `json:"sizeBytes"`
	RollbackRef string     `json:"rollbackRef"`
	Validation  Validation `json:"validation"`
}

type Writer struct {
	allowedRoot string
	rollbackDir string
}

func NewWriter(rollbackDir string) *Writer {
	return &Writer{allowedRoot: "/etc", rollbackDir: rollbackDir}
}

func (w *Writer) Write(ctx context.Context, path string, content []byte, expectedRevision, requestedMode, requestID string) (Result, error) {
	clean, err := w.validatePath(path)
	if err != nil {
		return Result{}, err
	}
	if len(content) > MaxWriteBytes {
		return Result{}, fmt.Errorf("%w: content exceeds the 1 MiB limit", ErrValidation)
	}
	if !validRevision(expectedRevision) {
		return Result{}, fmt.Errorf("%w: expected revision is required", ErrValidation)
	}
	info, err := os.Lstat(clean)
	if err != nil {
		return Result{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return Result{}, fmt.Errorf("%w: target must be a regular file, not a symlink", ErrValidation)
	}
	if info.Size() > MaxWriteBytes {
		return Result{}, fmt.Errorf("%w: target exceeds the 1 MiB limit", ErrValidation)
	}
	real, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return Result{}, err
	}
	if real != clean {
		return Result{}, fmt.Errorf("%w: symlinked paths are not allowed", ErrValidation)
	}
	oldContent, err := os.ReadFile(clean)
	if err != nil {
		return Result{}, err
	}
	actualRevision := revision(oldContent)
	if actualRevision != expectedRevision {
		return Result{}, &StaleError{Expected: expectedRevision, Actual: actualRevision}
	}
	mode, err := safeMode(requestedMode, info.Mode().Perm())
	if err != nil {
		return Result{}, err
	}
	dir := filepath.Dir(clean)
	tmp, err := os.CreateTemp(dir, ".lumio-privileged-*")
	if err != nil {
		return Result{}, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := io.Copy(tmp, bytes.NewReader(content)); err != nil {
		_ = tmp.Close()
		return Result{}, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return Result{}, err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return Result{}, err
	}
	uid, gid := ownership(info)
	if err := os.Chown(tmpPath, uid, gid); err != nil {
		_ = tmp.Close()
		return Result{}, err
	}
	if err := tmp.Close(); err != nil {
		return Result{}, err
	}
	validation, err := validateKnown(ctx, clean, tmpPath, content)
	if err != nil {
		return Result{}, err
	}
	rollbackRef, err := w.keepRollback(oldContent, info.Mode().Perm(), uid, gid, requestID)
	if err != nil {
		return Result{}, fmt.Errorf("create rollback copy: %w", err)
	}
	if err := os.Rename(tmpPath, clean); err != nil {
		return Result{}, err
	}
	syncDir(dir)
	return Result{
		Path:        clean,
		Revision:    revision(content),
		SizeBytes:   int64(len(content)),
		RollbackRef: rollbackRef,
		Validation:  validation,
	}, nil
}

func (w *Writer) validatePath(path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) {
		return "", fmt.Errorf("%w: path must be absolute", ErrValidation)
	}
	clean := filepath.Clean(path)
	if clean != path || !strings.HasPrefix(clean, w.allowedRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: only canonical paths below the protected root are allowed", ErrValidation)
	}
	realDir, err := filepath.EvalSymlinks(filepath.Dir(clean))
	if err != nil {
		return "", err
	}
	if realDir != filepath.Dir(clean) {
		return "", fmt.Errorf("%w: symlinked parent directories are not allowed", ErrValidation)
	}
	return clean, nil
}

func safeMode(requested string, existing fs.FileMode) (fs.FileMode, error) {
	if requested == "" {
		return existing, nil
	}
	value, err := strconv.ParseUint(requested, 8, 32)
	if err != nil || value > 0o644 || value&0o400 == 0 {
		return 0, fmt.Errorf("%w: mode must be a non-executable owner-readable mode no broader than 0644", ErrValidation)
	}
	return fs.FileMode(value), nil
}

func validRevision(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}

func revision(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (w *Writer) keepRollback(content []byte, mode fs.FileMode, uid, gid int, requestID string) (string, error) {
	if err := os.MkdirAll(w.rollbackDir, 0o700); err != nil {
		return "", err
	}
	requestHash := sha256.Sum256([]byte(requestID))
	ref := hex.EncodeToString(requestHash[:8]) + "-" + randomSuffix()
	tmp, err := os.CreateTemp(w.rollbackDir, ".rollback-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := os.Chown(tmpPath, uid, gid); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpPath, filepath.Join(w.rollbackDir, ref)); err != nil {
		return "", err
	}
	syncDir(w.rollbackDir)
	return ref, nil
}

func validateKnown(ctx context.Context, target, candidate string, content []byte) (Validation, error) {
	base := filepath.Base(target)
	var kind, command string
	var args []string
	switch {
	case strings.HasSuffix(target, ".json"):
		if !json.Valid(content) {
			return Validation{}, fmt.Errorf("%w: invalid JSON", ErrValidation)
		}
		return Validation{Kind: "json", Checked: true}, nil
	case base == "nginx.conf" || strings.Contains(target, "/nginx/"):
		kind, command, args = "nginx", "nginx", []string{"-t", "-c", candidate}
	case base == "sshd_config" || strings.Contains(target, "/ssh/sshd_config.d/"):
		kind, command, args = "sshd", "sshd", []string{"-t", "-f", candidate}
	case base == "sudoers" || strings.Contains(target, "/sudoers.d/"):
		kind, command, args = "sudoers", "visudo", []string{"-cf", candidate}
	case strings.Contains(target, "/systemd/system/"):
		kind, command, args = "systemd", "systemd-analyze", []string{"verify", candidate}
	default:
		return Validation{Kind: "none", Checked: false}, nil
	}
	path, err := exec.LookPath(command)
	if err != nil {
		return Validation{}, fmt.Errorf("%w: %s validator is unavailable", ErrValidation, kind)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if len(message) > 800 {
			message = message[len(message)-800:]
		}
		return Validation{}, fmt.Errorf("%w: %s validation failed: %s", ErrValidation, kind, message)
	}
	return Validation{Kind: kind, Checked: true}, nil
}

func ownership(info os.FileInfo) (int, int) {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return int(stat.Uid), int(stat.Gid)
	}
	return -1, -1
}

func syncDir(path string) {
	dir, err := os.Open(path)
	if err != nil {
		return
	}
	_ = dir.Sync()
	_ = dir.Close()
}

func randomSuffix() string {
	var value [8]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "fallback"
	}
	return hex.EncodeToString(value[:])
}
