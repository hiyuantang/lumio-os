// SPDX-License-Identifier: AGPL-3.0-only
//go:build darwin

package ipc

import (
	"net"
	"os"
)

func chmodSocket(path string, mode uint32) error {
	return os.Chmod(path, os.FileMode(mode))
}

func PeerCreds(conn net.Conn) (uid, pid uint32, err error) {
	return uint32(os.Getuid()), uint32(os.Getpid()), nil
}

func PeerUID(conn net.Conn) (uint32, error) {
	uid, _, err := PeerCreds(conn)
	return uid, err
}
