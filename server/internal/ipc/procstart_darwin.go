// SPDX-License-Identifier: AGPL-3.0-only
//go:build darwin

package ipc

func ProcStartTime(pid uint32) (uint64, error) {
	return 0, nil
}
