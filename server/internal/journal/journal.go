// SPDX-License-Identifier: AGPL-3.0-only
package journal

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var ErrValidation = errors.New("invalid journal query")
var ErrUnavailable = errors.New("journal is unavailable")

const (
	DefaultLimit = 200
	MaxLimit     = 1000
)

type Query struct {
	Unit     string
	Priority string
	Since    string
	Boot     string
	Limit    int
	After    string
}

type Entry struct {
	Cursor   string            `json:"cursor"`
	TS       string            `json:"ts"`
	Priority string            `json:"priority"`
	Unit     string            `json:"unit"`
	Message  string            `json:"message"`
	Fields   map[string]string `json:"fields,omitempty"`
}

type Result struct {
	Entries    []Entry `json:"entries"`
	NextCursor string  `json:"nextCursor"`
}

type Backend interface {
	Available() bool
	Query(ctx context.Context, q Query) (Result, error)
	Follow(ctx context.Context, q Query, emit func(Entry) bool) error
}

var priorityNames = map[string]bool{
	"emerg": true, "alert": true, "crit": true, "err": true,
	"warning": true, "notice": true, "info": true, "debug": true,
	"0": true, "1": true, "2": true, "3": true,
	"4": true, "5": true, "6": true, "7": true,
}

var unitNamePattern = regexp.MustCompile(`^[A-Za-z0-9:_.@-]+$`)

func (q Query) Validate() error {
	if q.Unit != "" && (strings.HasPrefix(q.Unit, "-") || !unitNamePattern.MatchString(q.Unit)) {
		return fmt.Errorf("%w: unit contains invalid characters", ErrValidation)
	}
	if q.Priority != "" && !priorityNames[strings.ToLower(q.Priority)] {
		return fmt.Errorf("%w: unknown priority %q", ErrValidation, q.Priority)
	}
	if q.Since != "" {
		if _, err := time.Parse(time.RFC3339, q.Since); err != nil {
			return fmt.Errorf("%w: since must be an RFC 3339 timestamp", ErrValidation)
		}
	}
	if q.Boot != "" && q.Boot != "current" && q.Boot != "previous" {
		return fmt.Errorf("%w: boot must be current or previous", ErrValidation)
	}
	if q.Limit < 0 || q.Limit > MaxLimit {
		return fmt.Errorf("%w: limit must be between 0 and %d", ErrValidation, MaxLimit)
	}
	if strings.HasPrefix(q.After, "-") || len(q.After) > 8192 {
		return fmt.Errorf("%w: invalid cursor", ErrValidation)
	}
	return nil
}

func (q Query) limit() int {
	if q.Limit <= 0 {
		return DefaultLimit
	}
	return q.Limit
}
