// SPDX-License-Identifier: AGPL-3.0-only
package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadUnitFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.service")
	if err := os.WriteFile(path, []byte("[Service]\nExecStart=/usr/bin/demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := readUnitFile(path, true)
	if file.Path != path || !file.Override || file.Error != "" || !strings.Contains(file.Content, "ExecStart=/usr/bin/demo") {
		t.Fatalf("file = %+v", file)
	}
}

func TestReadUnitFileRejectsOversizedContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.service")
	if err := os.WriteFile(path, make([]byte, maxUnitFileSize+1), 0o644); err != nil {
		t.Fatal(err)
	}
	file := readUnitFile(path, false)
	if file.Content != "" || file.Error == "" {
		t.Fatalf("file = %+v", file)
	}
}
