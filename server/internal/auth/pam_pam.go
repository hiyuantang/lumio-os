// SPDX-License-Identifier: AGPL-3.0-only
//go:build pam

package auth

import (
	"fmt"

	"github.com/msteinert/pam/v2"
)

const Available = true

func Authenticate(username, password string) error {
	tx, err := pam.StartFunc("lumiod", username, func(style pam.Style, msg string) (string, error) {
		if style == pam.PromptEchoOff {
			return password, nil
		}
		return "", fmt.Errorf("unexpected PAM prompt style %d", style)
	})
	if err != nil {
		return fmt.Errorf("pam start: %w", err)
	}
	if err := tx.Authenticate(0); err != nil {
		return err
	}
	if err := tx.AcctMgmt(0); err != nil {
		return err
	}
	return nil
}
