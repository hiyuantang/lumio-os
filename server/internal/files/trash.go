// SPDX-License-Identifier: AGPL-3.0-only
package files

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type TrashResult struct {
	Trashed bool `json:"trashed"`
}

func Trash(p string) (TrashResult, error) {
	clean, err := cleanPath(p)
	if err != nil {
		return TrashResult{}, err
	}
	real, err := resolve(clean)
	if err != nil {
		return TrashResult{}, err
	}
	if real == string(filepath.Separator) {
		return TrashResult{}, fmt.Errorf("%w: cannot trash the root directory", ErrValidation)
	}
	trashDir, err := trashDir()
	if err != nil {
		return TrashResult{}, err
	}
	filesDir := filepath.Join(trashDir, "files")
	infoDir := filepath.Join(trashDir, "info")
	for _, d := range []string{filesDir, infoDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return TrashResult{}, fmt.Errorf("%w: trash is not writable: %v", ErrValidation, err)
		}
	}
	if resolvedTrash, err := filepath.EvalSymlinks(trashDir); err == nil {
		if real == resolvedTrash || strings.HasPrefix(real, resolvedTrash+string(filepath.Separator)) {
			return TrashResult{}, fmt.Errorf("%w: path is already in the trash", ErrValidation)
		}
	}

	unlock := writeLocks.lock(real)
	defer unlock()

	name := filepath.Base(real)
	dest := uniqueTrashName(filesDir, name)
	if err := os.Rename(real, dest); err != nil {
		if errors.Is(err, syscall.EXDEV) {
			return TrashResult{}, fmt.Errorf("%w: cannot trash across filesystems", ErrValidation)
		}
		return TrashResult{}, err
	}
	info := "[Trash Info]\nPath=" + trashEscape(real) + "\nDeletionDate=" + time.Now().Format("2006-01-02T15:04:05") + "\n"
	infoPath := filepath.Join(infoDir, filepath.Base(dest)+".trashinfo")
	if err := os.WriteFile(infoPath, []byte(info), 0o600); err != nil {
		return TrashResult{}, err
	}
	return TrashResult{Trashed: true}, nil
}

func trashDir() (string, error) {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "Trash"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("%w: no home directory available for trash", ErrValidation)
	}
	return filepath.Join(home, ".local", "share", "Trash"), nil
}

func uniqueTrashName(filesDir, name string) string {
	candidate := filepath.Join(filesDir, name)
	for i := 1; ; i++ {
		if _, err := os.Lstat(candidate); os.IsNotExist(err) {
			return candidate
		}
		candidate = filepath.Join(filesDir, fmt.Sprintf("%s.%d", name, i))
	}
}

func trashEscape(p string) string {
	return strings.ReplaceAll(url.PathEscape(p), "%2F", "/")
}
