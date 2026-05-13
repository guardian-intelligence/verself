package iam

import (
	"testing"
	"time"
)

func TestFixedWindowOperationRateLimiter(t *testing.T) {
	limiter := NewFixedWindowOperationRateLimiter(map[RateLimitClass]RateLimitRule{
		"mutation": {Limit: 2, Window: time.Minute},
	})
	now := time.Unix(1700000000, 0)
	if decision := limiter.Allow("mutation", "org\x00subject", now); !decision.Allowed {
		t.Fatalf("first request should be allowed: %#v", decision)
	}
	if decision := limiter.Allow("mutation", "org\x00subject", now.Add(time.Second)); !decision.Allowed {
		t.Fatalf("second request should be allowed: %#v", decision)
	}
	if decision := limiter.Allow("mutation", "org\x00subject", now.Add(2*time.Second)); decision.Allowed || decision.RetryAfter <= 0 {
		t.Fatalf("third request should be throttled: %#v", decision)
	}
	if decision := limiter.Allow("mutation", "org\x00subject", now.Add(time.Minute)); !decision.Allowed {
		t.Fatalf("next window should be allowed: %#v", decision)
	}
}
