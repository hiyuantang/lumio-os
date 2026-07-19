// SPDX-License-Identifier: AGPL-3.0-only
//go:build linux

package ipc

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func ProcStartTime(pid uint32) (uint64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	content := string(data)
	close := strings.LastIndex(content, ")")
	if close < 0 {
		return 0, fmt.Errorf("malformed stat for pid %d", pid)
	}
	fields := strings.Fields(content[close+1:])
	if len(fields) < 20 {
		return 0, fmt.Errorf("short stat for pid %d", pid)
	}
	return strconv.ParseUint(fields[19], 10, 64)
}
