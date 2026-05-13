package iam

import (
	"sync"
	"time"
)

const defaultRateLimiterMaxWindowEntries = 10000

// RateLimitRule is a service-owned fixed-window throttle rule for an
// OperationPolicy rate-limit class.
type RateLimitRule struct {
	Limit  int
	Window time.Duration
}

// RateLimitDecision is the result of checking an operation throttle.
type RateLimitDecision struct {
	Allowed    bool
	RetryAfter time.Duration
}

type rateLimitWindow struct {
	ResetAt time.Time
	Count   int
}

// FixedWindowOperationRateLimiter enforces per-service OperationPolicy
// rate-limit classes without introducing a shared cross-service quota store.
type FixedWindowOperationRateLimiter struct {
	mu               sync.Mutex
	rules            map[RateLimitClass]RateLimitRule
	windows          map[string]rateLimitWindow
	maxWindowEntries int
}

// NewFixedWindowOperationRateLimiter creates a limiter with a bounded in-memory
// window table. Services remain responsible for choosing conservative keys.
func NewFixedWindowOperationRateLimiter(rules map[RateLimitClass]RateLimitRule) *FixedWindowOperationRateLimiter {
	copied := make(map[RateLimitClass]RateLimitRule, len(rules))
	for class, rule := range rules {
		copied[class] = rule
	}
	return &FixedWindowOperationRateLimiter{
		rules:            copied,
		windows:          map[string]rateLimitWindow{},
		maxWindowEntries: defaultRateLimiterMaxWindowEntries,
	}
}

// Allow checks whether key can execute class at now.
func (l *FixedWindowOperationRateLimiter) Allow(class RateLimitClass, key string, now time.Time) RateLimitDecision {
	if l == nil || class == "" {
		return RateLimitDecision{Allowed: true}
	}
	rule, ok := l.rules[class]
	if !ok || rule.Limit <= 0 || rule.Window <= 0 {
		return RateLimitDecision{Allowed: true}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.windows) > l.maxWindowEntries {
		l.pruneExpired(now)
	}

	key = string(class) + "\x00" + key
	window := l.windows[key]
	if window.ResetAt.IsZero() || !now.Before(window.ResetAt) {
		l.windows[key] = rateLimitWindow{ResetAt: now.Add(rule.Window), Count: 1}
		return RateLimitDecision{Allowed: true}
	}
	if window.Count >= rule.Limit {
		return RateLimitDecision{Allowed: false, RetryAfter: window.ResetAt.Sub(now).Round(time.Second)}
	}
	window.Count++
	l.windows[key] = window
	return RateLimitDecision{Allowed: true}
}

func (l *FixedWindowOperationRateLimiter) pruneExpired(now time.Time) {
	for key, window := range l.windows {
		if !now.Before(window.ResetAt) {
			delete(l.windows, key)
		}
	}
}
