// SPDX-License-Identifier: AGPL-3.0-only
//go:build !pam

package auth

const Available = false

func Authenticate(username, password string) error {
	return ErrUnavailable
}
