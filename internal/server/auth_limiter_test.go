package server

import (
	"fmt"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestAttemptLimiterIsPerKeyTokenBucket(t *testing.T) {
	now := time.Date(2026, 10, 18, 8, 0, 0, 0, time.UTC)
	limiter := newAttemptLimiter()
	limiter.window = 5 * time.Minute
	limiter.maxEntries = 8
	limiter.now = func() time.Time { return now }

	const key = "login:203.0.113.10"
	for range 3 {
		if ok, _ := limiter.allow(key, 3); !ok {
			t.Fatal("initial attempts should be allowed")
		}
	}
	if ok, retry := limiter.allow(key, 3); ok || retry < 99*time.Second || retry > 101*time.Second {
		t.Fatalf("fourth attempt = ok:%v retry:%v, want denied for about 100s", ok, retry)
	}
	if ok, _ := limiter.allow("login:203.0.113.11", 3); !ok {
		t.Fatal("one source exhausted another source's bucket")
	}

	now = now.Add(100 * time.Second)
	if ok, _ := limiter.allow(key, 3); !ok {
		t.Fatal("refilled bucket should allow one attempt")
	}
}

func TestAttemptLimiterMemoryIsBounded(t *testing.T) {
	now := time.Date(2026, 10, 18, 8, 0, 0, 0, time.UTC)
	limiter := newAttemptLimiter()
	limiter.window = 5 * time.Minute
	limiter.maxEntries = 64
	limiter.now = func() time.Time { return now }

	for i := range 10_000 {
		limiter.allow(fmt.Sprintf("login:2001:db8::%x", i), 8)
	}

	limiter.mu.Lock()
	count := len(limiter.entries)
	limiter.mu.Unlock()
	if count > limiter.maxEntries {
		t.Fatalf("limiter retained %d entries, max is %d", count, limiter.maxEntries)
	}
}

func TestAttemptLimiterConcurrentAccessStaysBounded(t *testing.T) {
	now := time.Date(2026, 10, 18, 8, 0, 0, 0, time.UTC)
	limiter := newAttemptLimiter()
	limiter.window = 5 * time.Minute
	limiter.maxEntries = 256
	limiter.now = func() time.Time { return now }

	var wg sync.WaitGroup
	for worker := range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 256 {
				limiter.allow(fmt.Sprintf("login:%d:%d", worker, i), 8)
			}
		}()
	}
	wg.Wait()

	limiter.mu.Lock()
	count := len(limiter.entries)
	limiter.mu.Unlock()
	if count > limiter.maxEntries {
		t.Fatalf("limiter retained %d entries, max is %d", count, limiter.maxEntries)
	}
}

func TestAttemptLimiterDropsExpiredEntriesAtCapacity(t *testing.T) {
	now := time.Date(2026, 10, 18, 8, 0, 0, 0, time.UTC)
	limiter := newAttemptLimiter()
	limiter.window = 5 * time.Minute
	limiter.maxEntries = 8
	limiter.now = func() time.Time { return now }

	for i := range limiter.maxEntries {
		limiter.allow(fmt.Sprintf("setup:192.0.2.%d", i), 2)
	}
	now = now.Add(6 * time.Minute)
	limiter.allow("setup:192.0.2.200", 2)

	limiter.mu.Lock()
	count := len(limiter.entries)
	limiter.mu.Unlock()
	if count != 1 {
		t.Fatalf("entries after expiry cleanup = %d, want 1", count)
	}
}

func TestClientInfoCanonicalizesTrustedSourceAddress(t *testing.T) {
	srv := &Server{TrustProxy: true}
	req := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	req.RemoteAddr = "127.0.0.1:4321"
	req.Header.Set("X-Forwarded-For", " 2001:0db8::1, 10.0.0.2")
	if got := srv.clientInfo(req).IP; got != "2001:db8::1" {
		t.Fatalf("trusted source IP = %q, want canonical IPv6", got)
	}

	req.Header.Set("X-Forwarded-For", "not-an-ip")
	if got := srv.clientInfo(req).IP; got != "not-an-ip" {
		t.Fatalf("malformed forwarded source = %q, want fail-closed value", got)
	}
	if srv.localEnrollment(req) {
		t.Fatal("malformed forwarded source must not inherit the proxy's loopback status")
	}
}
