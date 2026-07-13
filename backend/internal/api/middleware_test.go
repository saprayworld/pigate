package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestSecurityHeaders verifies the hardening headers are present on an
// unauthenticated API 401 (proving the middleware sits outside auth in the
// chain) and on an authenticated API 200. Note: an OPTIONS preflight is
// deliberately NOT checked — CORS is the outermost middleware and short-circuits
// preflights (204) before SecurityHeaders runs, which is fine since a preflight
// renders no document.
func TestSecurityHeaders(t *testing.T) {
	handler, _ := setupTestServer(t)

	authedReq := httptest.NewRequest("GET", "/api/qos/rules", nil)
	addSessionCookie(authedReq, "mock_session_id_test_token")

	cases := []struct {
		name string
		req  *http.Request
	}{
		{"api 401 (no cookie)", httptest.NewRequest("GET", "/api/auth/session", nil)},
		{"api 200 (authenticated)", authedReq},
		// The SPA HTML document is the response the clickjacking/CSP protection
		// exists for — assert the static-serving path is covered by the
		// middleware too (headers must be present whatever status serveStatic
		// returns for the embedded dist in the test build).
		{"spa index (GET /)", httptest.NewRequest("GET", "/", nil)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, tc.req)
			h := rec.Header()
			if got := h.Get("Content-Security-Policy"); got != cspPolicy {
				t.Errorf("CSP = %q, want %q", got, cspPolicy)
			}
			if got := h.Get("X-Frame-Options"); got != "DENY" {
				t.Errorf("X-Frame-Options = %q, want DENY", got)
			}
			if got := h.Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
			}
			if got := h.Get("Referrer-Policy"); got != "no-referrer" {
				t.Errorf("Referrer-Policy = %q, want no-referrer", got)
			}
		})
	}
}

// TestBodyLimitMiddleware verifies an oversized body to a normal endpoint fails
// with a 4xx (not a panic / not buffered), while the config/import path is
// exempt from the 1 MB cap.
func TestBodyLimitMiddleware(t *testing.T) {
	handler, _ := setupTestServer(t)

	t.Run("oversized body to normal endpoint -> 4xx", func(t *testing.T) {
		big := bytes.Repeat([]byte("a"), maxRequestBody+1024) // > 1 MB
		req := httptest.NewRequest("POST", "/api/qos/rules", bytes.NewReader(big))
		req.Header.Set("Content-Type", "application/json")
		addSessionCookie(req, "mock_session_id_test_token")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code < 400 || rec.Code >= 500 {
			t.Fatalf("expected 4xx for oversized body, got %d", rec.Code)
		}
	})

	t.Run("import path is exempt from 1 MB cap", func(t *testing.T) {
		// A ~2 MB body must NOT be truncated at 1 MB by the middleware. The import
		// handler installs its own 10 MB cap and will fail later on decrypt/parse,
		// not on a MaxBytesReader read error — so we must not see the 1 MB limit
		// surface as a read failure. We assert it does not return the generic
		// "max 10 MB" read-error path prematurely.
		big := bytes.Repeat([]byte("a"), 2<<20) // 2 MB, over 1 MB but under 10 MB
		req := httptest.NewRequest("POST", "/api/system/config/import", bytes.NewReader(big))
		addSessionCookie(req, "mock_session_id_test_token")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		// The 2 MB body was fully read (import fails on content, not on the 1 MB
		// cap). A truncated read would have produced the import read-error message.
		if strings.Contains(rec.Body.String(), "max 10 MB") {
			t.Fatalf("import body appears to have hit a read cap: %s", rec.Body.String())
		}
	})
}

// TestRateLimiterAllow verifies the token bucket: 5 allowed, 6th denied.
func TestRateLimiterAllow(t *testing.T) {
	resetLimiters()
	lim := getLimiter("10.0.0.1")
	for i := 0; i < 5; i++ {
		if !lim.allow() {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if lim.allow() {
		t.Fatal("6th request should be denied")
	}
}

// TestRateLimiterLastSeenAdvances guards the trap: lastSeen must advance on every
// allow() even when requests arrive faster than the 2s refill interval (so an
// active bucket is never mistaken for idle and evicted mid-attack).
func TestRateLimiterLastSeenAdvances(t *testing.T) {
	resetLimiters()
	lim := getLimiter("10.0.0.2")
	lim.allow()
	first := lim.lastSeen
	time.Sleep(5 * time.Millisecond)
	lim.allow() // second call within <2s: `last` won't move, `lastSeen` must
	if !lim.lastSeen.After(first) {
		t.Fatal("lastSeen must advance on every allow(), even sub-2s bursts")
	}
}

// TestSweepIdleLimiters removes only idle entries, keeping active ones.
func TestSweepIdleLimiters(t *testing.T) {
	resetLimiters()
	idle := getLimiter("10.0.0.3")
	active := getLimiter("10.0.0.4")
	// Force one entry to look idle.
	idle.mu.Lock()
	idle.lastSeen = time.Now().Add(-2 * limiterIdleAfter)
	idle.mu.Unlock()
	_ = active

	removed := sweepIdleLimiters()
	if removed != 1 {
		t.Fatalf("expected 1 entry swept, got %d", removed)
	}
	limitersMu.Lock()
	_, idleExists := limiters["10.0.0.3"]
	_, activeExists := limiters["10.0.0.4"]
	limitersMu.Unlock()
	if idleExists {
		t.Error("idle entry should have been evicted")
	}
	if !activeExists {
		t.Error("active entry must survive the sweep")
	}
}

// TestLimiterHardCap ensures the map never exceeds maxLimiterEntries even when
// flooded with unique (all-active) source IPs.
func TestLimiterHardCap(t *testing.T) {
	resetLimiters()
	for i := 0; i < maxLimiterEntries+500; i++ {
		getLimiter(fmt.Sprintf("10.1.%d.%d", i/256, i%256))
	}
	limitersMu.Lock()
	n := len(limiters)
	limitersMu.Unlock()
	if n > maxLimiterEntries {
		t.Fatalf("limiter map exceeded hard cap: %d > %d", n, maxLimiterEntries)
	}
}

// TestLimiterCapSweepTimeGated verifies the at-cap idle sweep is rate-limited:
// within limiterCapSweepMinGap of the last sweep, an insert at the cap must NOT
// rescan the map (idle entries survive; one random eviction makes room), while
// after the gap the sweep runs and reaps ALL idle entries. Distinguished by
// count: with 2 idle entries at cap, the O(1) random path leaves len == cap
// (-1 evicted, +1 inserted) but a real sweep leaves len == cap-1 (-2 idle, +1).
func TestLimiterCapSweepTimeGated(t *testing.T) {
	fillToCap := func() {
		resetLimiters()
		for i := 0; len(limiters) < maxLimiterEntries; i++ {
			getLimiter(fmt.Sprintf("10.2.%d.%d", i/256, i%256))
		}
		// Make exactly two entries idle.
		for _, ip := range []string{"10.2.0.1", "10.2.0.2"} {
			lim := limiters[ip]
			lim.mu.Lock()
			lim.lastSeen = time.Now().Add(-2 * limiterIdleAfter)
			lim.mu.Unlock()
		}
	}

	t.Run("sweep skipped within the gap", func(t *testing.T) {
		fillToCap()
		limitersMu.Lock()
		lastCapSweep = time.Now() // pretend a sweep just ran
		limitersMu.Unlock()

		getLimiter("192.0.2.1")
		limitersMu.Lock()
		n := len(limiters)
		limitersMu.Unlock()
		if n != maxLimiterEntries {
			t.Fatalf("gated insert should use the O(1) eviction (len == cap), got len %d", n)
		}
	})

	t.Run("sweep runs after the gap", func(t *testing.T) {
		fillToCap()
		limitersMu.Lock()
		lastCapSweep = time.Now().Add(-2 * limiterCapSweepMinGap)
		limitersMu.Unlock()

		getLimiter("192.0.2.2")
		limitersMu.Lock()
		n := len(limiters)
		limitersMu.Unlock()
		if n != maxLimiterEntries-1 {
			t.Fatalf("ungated insert should sweep both idle entries (len == cap-1), got len %d", n)
		}
	})
}

// TestEvictedLimiterNeverLocksOut verifies the safety property: a bucket removed
// mid-use resets to a full 5 tokens, so eviction can only relax the limit.
func TestEvictedLimiterNeverLocksOut(t *testing.T) {
	resetLimiters()
	lim := getLimiter("10.0.0.9")
	lim.allow()
	lim.allow() // 3 tokens left

	limitersMu.Lock()
	delete(limiters, "10.0.0.9")
	limitersMu.Unlock()

	fresh := getLimiter("10.0.0.9")
	if !fresh.allow() {
		t.Fatal("a re-created bucket must start full and allow the next request")
	}
	if fresh.tokens != 4 {
		t.Fatalf("re-created bucket should have 5 tokens then spend 1 (=4), got %d", fresh.tokens)
	}
}

// TestRateLimitMiddlewareIPv6 ensures a bracketed IPv6 RemoteAddr is parsed into
// a clean key without crashing.
func TestRateLimitMiddlewareIPv6(t *testing.T) {
	resetLimiters()
	called := false
	h := RateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	req := httptest.NewRequest("POST", "/api/auth/login", nil)
	req.RemoteAddr = "[2001:db8::1]:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !called {
		t.Fatal("first IPv6 request should pass through")
	}
	limitersMu.Lock()
	_, ok := limiters["2001:db8::1"]
	limitersMu.Unlock()
	if !ok {
		t.Fatalf("expected limiter keyed by bare IPv6 host, keys=%v", limiterKeys())
	}
}

// resetLimiters clears the global limiter map (and the at-cap sweep gate) so
// tests are independent.
func resetLimiters() {
	limitersMu.Lock()
	limiters = make(map[string]*rateLimiter)
	lastCapSweep = time.Time{}
	limitersMu.Unlock()
}

func limiterKeys() []string {
	limitersMu.Lock()
	defer limitersMu.Unlock()
	keys := make([]string, 0, len(limiters))
	for k := range limiters {
		keys = append(keys, k)
	}
	return keys
}

// TestQosAuditEvent verifies creating a QoS rule records a central audit event
// carrying the acting user (finding 11 gap: QoS previously logged nothing).
func TestQosAuditEvent(t *testing.T) {
	server, repo := buildTestServer(t, false)
	handler := RegisterRoutes(server)
	AddSession("mock_session_id_test_token", "pigate")

	rule := map[string]interface{}{
		"name":      "audit-test",
		"interface": "eth0",
		"status":    true,
	}
	body, _ := json.Marshal(rule)
	req := httptest.NewRequest("POST", "/api/qos/rules", bytes.NewReader(body))
	addSessionCookie(req, "mock_session_id_test_token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	// Events are queued in RAM; flush synchronously then read them back.
	if err := server.eventLog.Flush(); err != nil {
		t.Fatalf("flush event log: %v", err)
	}
	events, err := repo.GetSystemEvents("qos", "", "", 50, 0)
	if err != nil {
		t.Fatalf("query events: %v", err)
	}
	found := false
	for _, e := range events {
		if e.Action == "qos.rule_created" && e.Actor == "pigate" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a qos.rule_created event with actor 'pigate', got %+v", events)
	}
}

// TestGenerateRandomToken verifies the fail-closed helper returns unique,
// non-empty tokens (the error path is only hit if the OS entropy source fails,
// which we can't inject portably; the signature change itself is the fix).
func TestGenerateRandomToken(t *testing.T) {
	a, err := generateRandomToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := generateRandomToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a == "" || b == "" {
		t.Fatal("token must not be empty")
	}
	if a == b {
		t.Fatal("tokens must be unique")
	}
	if len(a) != 32 { // 16 bytes hex-encoded
		t.Fatalf("expected 32 hex chars, got %d", len(a))
	}

	id, err := randomID("rule-")
	if err != nil {
		t.Fatalf("randomID error: %v", err)
	}
	if !strings.HasPrefix(id, "rule-") || len(id) != len("rule-")+8 {
		t.Fatalf("unexpected randomID form: %q", id)
	}
}
