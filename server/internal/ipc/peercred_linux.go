// SPDX-License-Identifier: AGPL-3.0-only
//go:build linux

package ipc

import (
	"fmt"
	"net"
	"os"
	"syscall"
)

func chmodSocket(path string, mode uint32) error {
	return os.Chmod(path, os.FileMode(mode))
}

func PeerCreds(conn net.Conn) (uid, pid uint32, err error) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, 0, fmt.Errorf("not a unix connection")
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, 0, err
	}
	var cred *syscall.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return 0, 0, err
	}
	if credErr != nil {
		return 0, 0, credErr
	}
	return uint32(cred.Uid), uint32(cred.Pid), nil
}

func PeerUID(conn net.Conn) (uint32, error) {
	uid, _, err := PeerCreds(conn)
	return uid, err
}
