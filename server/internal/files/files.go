// SPDX-License-Identifier: AGPL-3.0-only
package files

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
	"unicode/utf8"
)

var ErrValidation = errors.New("invalid file request")

const (
	MaxReadBytes    = 1 << 20
	detectByteCount = 8000
)

type ListEntry struct {
	Name          string  `json:"name"`
	Type          string  `json:"type"`
	SizeBytes     int64   `json:"sizeBytes"`
	Mode          string  `json:"mode"`
	ModifiedAt    string  `json:"modifiedAt"`
	SymlinkTarget *string `json:"symlinkTarget"`
}

type ListResult struct {
	Path    string      `json:"path"`
	Entries []ListEntry `json:"entries"`
}

type ReadResult struct {
	Path      string  `json:"path"`
	SizeBytes int64   `json:"sizeBytes"`
	Revision  string  `json:"revision"`
	Encoding  string  `json:"encoding"`
	Content   *string `json:"content"`
	Truncated bool    `json:"truncated"`
}

func cleanPath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("%w: path is required", ErrValidation)
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("%w: path must be absolute", ErrValidation)
	}
	return filepath.Clean(p), nil
}

func resolve(clean string) (string, error) {
	real, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "", err
	}
	return real, nil
}

func List(p string) (ListResult, error) {
	clean, err := cleanPath(p)
	if err != nil {
		return ListResult{}, err
	}
	real, err := resolve(clean)
	if err != nil {
		return ListResult{}, err
	}
	info, err := os.Stat(real)
	if err != nil {
		return ListResult{}, err
	}
	if !info.IsDir() {
		return ListResult{}, fmt.Errorf("%w: path is not a directory", ErrValidation)
	}
	dirEntries, err := os.ReadDir(real)
	if err != nil {
		return ListResult{}, err
	}
	entries := []ListEntry{}
	for _, de := range dirEntries {
		info, err := de.Info()
		if err != nil {
			continue
		}
		entry := ListEntry{
			Name:       de.Name(),
			Type:       entryType(info),
			SizeBytes:  info.Size(),
			Mode:       fmt.Sprintf("%04o", info.Mode().Perm()),
			ModifiedAt: info.ModTime().UTC().Format(time.RFC3339),
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if target, err := os.Readlink(filepath.Join(real, de.Name())); err == nil {
				entry.SymlinkTarget = &target
			}
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return ListResult{Path: clean, Entries: entries}, nil
}

func Read(p string) (ReadResult, error) {
	clean, err := cleanPath(p)
	if err != nil {
		return ReadResult{}, err
	}
	real, err := resolve(clean)
	if err != nil {
		return ReadResult{}, err
	}
	f, err := os.Open(real)
	if err != nil {
		return ReadResult{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ReadResult{}, err
	}
	if info.IsDir() {
		return ReadResult{}, fmt.Errorf("%w: path is a directory", ErrValidation)
	}
	hasher := sha256.New()
	head, err := io.ReadAll(io.LimitReader(io.TeeReader(f, hasher), MaxReadBytes+1))
	if err != nil {
		return ReadResult{}, err
	}
	truncated := len(head) > MaxReadBytes
	if truncated {
		head = head[:MaxReadBytes]
	}
	if _, err := io.Copy(hasher, f); err != nil {
		return ReadResult{}, err
	}
	res := ReadResult{
		Path:      clean,
		SizeBytes: info.Size(),
		Revision:  "sha256:" + hex.EncodeToString(hasher.Sum(nil)),
		Truncated: truncated,
	}
	if encoding := detectEncoding(head); encoding == "binary" {
		res.Encoding = "binary"
	} else {
		res.Encoding = "utf-8"
		content := base64.StdEncoding.EncodeToString(head)
		res.Content = &content
	}
	return res, nil
}

func entryType(info os.FileInfo) string {
	mode := info.Mode()
	switch {
	case mode.IsDir():
		return "directory"
	case mode&os.ModeSymlink != 0:
		return "symlink"
	case mode.IsRegular():
		return "file"
	default:
		return "other"
	}
}

func detectEncoding(head []byte) string {
	sample := head
	if len(sample) > detectByteCount {
		sample = sample[:detectByteCount]
	}
	if bytes.IndexByte(sample, 0) >= 0 {
		return "binary"
	}
	if utf8.Valid(sample) {
		return "utf-8"
	}
	for i := 1; i <= 3 && i < len(sample); i++ {
		if utf8.Valid(sample[:len(sample)-i]) {
			return "utf-8"
		}
	}
	return "binary"
}
