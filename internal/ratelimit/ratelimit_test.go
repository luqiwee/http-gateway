package ratelimit

import (
	"testing"
	"time"
)

func TestManagerAllowUsesTokenBucket(t *testing.T) {
	now := time.Unix(100, 0)
	m := NewManager()
	m.now = func() time.Time { return now }

	if !m.Allow("route", 1, 2) {
		t.Fatal("first request should pass")
	}
	if !m.Allow("route", 1, 2) {
		t.Fatal("second request should pass")
	}
	if m.Allow("route", 1, 2) {
		t.Fatal("third request should be limited")
	}

	now = now.Add(time.Second)
	if !m.Allow("route", 1, 2) {
		t.Fatal("request after refill should pass")
	}
}
