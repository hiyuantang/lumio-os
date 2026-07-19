// SPDX-License-Identifier: AGPL-3.0-only
package gateway

import (
	"sync"
	"time"
)

const (
	limiterThreshold = 3
	limiterMaxDelay  = 60 * time.Second
)

type loginLimiter struct {
	mu     sync.Mutex
	failed map[string]*limiterEntry
}

type limiterEntry struct {
	count   int
	blocked time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{failed: map[string]*limiterEntry{}}
}

func (l *loginLimiter) blocked(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.failed[key]
	if !ok {
		return 0
	}
	if remaining := time.Until(entry.blocked); remaining > 0 {
		return remaining
	}
	return 0
}

func (l *loginLimiter) record(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.failed[key]
	if !ok {
		entry = &limiterEntry{}
		l.failed[key] = entry
	}
	entry.count++
	if entry.count >= limiterThreshold {
		delay := time.Duration(1<<(entry.count-limiterThreshold+1)) * time.Second
		if delay > limiterMaxDelay {
			delay = limiterMaxDelay
		}
		entry.blocked = time.Now().Add(delay)
	}
}

func (l *loginLimiter) reset(key string) {
	l.mu.Lock()
	delete(l.failed, key)
	l.mu.Unlock()
}
