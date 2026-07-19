// SPDX-License-Identifier: AGPL-3.0-only
package auth

import "errors"

var ErrUnavailable = errors.New("PAM authentication is not available in this build")

type Result struct {
	UID uint32
	GID uint32
}
