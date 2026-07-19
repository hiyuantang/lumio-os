// SPDX-License-Identifier: AGPL-3.0-only
//go:build darwin

package sessiond

import (
	"fmt"
	"os"
	"os/exec"
)

func setSpawnCredential(cmd *exec.Cmd, uid, gid uint32, groups []uint32) error {
	if uint32(os.Geteuid()) != uid {
		return fmt.Errorf("cannot spawn agent as uid %d without root on this platform (dev supports same-user only)", uid)
	}
	return nil
}
