// SPDX-License-Identifier: AGPL-3.0-only
//go:build linux

package sessiond

import (
	"os/exec"
	"syscall"
)

func setSpawnCredential(cmd *exec.Cmd, uid, gid uint32, groups []uint32) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: uid, Gid: gid, Groups: groups},
	}
	return nil
}
