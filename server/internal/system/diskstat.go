// SPDX-License-Identifier: AGPL-3.0-only
//go:build linux || darwin

package system

import "syscall"

func statfs(path string) (total uint64, free uint64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bsize := uint64(st.Bsize)
	return st.Blocks * bsize, st.Bfree * bsize, nil
}
