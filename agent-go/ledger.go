package main

import (
	"sync"
	"time"
)

// commandLedger remembers which commandId values were already handled so an
// at-least-once queue cannot run an action twice. It is bounded by both entry age
// and entry count, so an agent that runs for months never grows without limit.
type commandLedger struct {
	mutex   sync.Mutex
	window  time.Duration
	maxSize int
	seen    map[string]time.Time
	order   []string
}

func newCommandLedger(window time.Duration, maxSize int) *commandLedger {
	return &commandLedger{window: window, maxSize: maxSize, seen: map[string]time.Time{}}
}

// admit reports whether this commandId may run now. It returns false for a
// commandId that is still remembered, and records the id otherwise.
func (l *commandLedger) admit(commandID string, now time.Time) bool {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.forgetExpired(now)
	if _, repeated := l.seen[commandID]; repeated {
		return false
	}
	l.seen[commandID] = now
	l.order = append(l.order, commandID)
	for len(l.order) > l.maxSize {
		l.forget(l.order[0])
	}
	return true
}

func (l *commandLedger) size() int {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	return len(l.seen)
}

func (l *commandLedger) forgetExpired(now time.Time) {
	for len(l.order) > 0 {
		if now.Sub(l.seen[l.order[0]]) < l.window {
			return
		}
		l.forget(l.order[0])
	}
}

func (l *commandLedger) forget(commandID string) {
	delete(l.seen, commandID)
	l.order = l.order[1:]
}
