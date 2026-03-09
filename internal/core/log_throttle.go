package core

import (
	"strings"
	"sync"
	"time"
)

type LogThrottle struct {
	mu      sync.Mutex
	lastAt  map[string]time.Time
	maxKeys int
	maxAge  time.Duration
}

func NewLogThrottle(maxKeys int, maxAge time.Duration) *LogThrottle {
	if maxKeys <= 0 {
		maxKeys = 1
	}
	if maxAge <= 0 {
		maxAge = time.Minute
	}
	return &LogThrottle{
		lastAt:  make(map[string]time.Time),
		maxKeys: maxKeys,
		maxAge:  maxAge,
	}
}

func (t *LogThrottle) Allow(key string, interval time.Duration, now time.Time) bool {
	if t == nil {
		return false
	}
	key = strings.TrimSpace(key)
	if key == "" {
		key = "default"
	}
	if now.IsZero() {
		now = time.Now()
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if interval > 0 {
		if last, ok := t.lastAt[key]; ok && now.Sub(last) < interval {
			return false
		}
	}
	t.lastAt[key] = now
	t.pruneLocked(now)
	return true
}

func (t *LogThrottle) pruneLocked(now time.Time) {
	if len(t.lastAt) <= t.maxKeys {
		return
	}

	for key, ts := range t.lastAt {
		if now.Sub(ts) > t.maxAge {
			delete(t.lastAt, key)
		}
	}

	for len(t.lastAt) > t.maxKeys {
		oldestKey := ""
		oldestTime := now
		for key, ts := range t.lastAt {
			if ts.Before(oldestTime) {
				oldestKey = key
				oldestTime = ts
			}
		}
		if oldestKey == "" {
			break
		}
		delete(t.lastAt, oldestKey)
	}
}
