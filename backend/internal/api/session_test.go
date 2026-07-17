package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// hasSessionCookie reports whether the recorder carries a pigate_session
// Set-Cookie (i.e. the cookie was re-issued).
func hasSessionCookie(rec *httptest.ResponseRecorder) bool {
	for _, c := range rec.Result().Cookies() {
		if c.Name == SessionKey {
			return true
		}
	}
	return false
}

func TestValidateSessionExpired(t *testing.T) {
	token := "sess-expired"
	now := time.Now()
	addSessionWithTimes(token, "u1", now.Add(-sessionTTL), now.Add(-time.Second))
	defer RemoveSession(token)

	if _, _, ok := ValidateSession(token); ok {
		t.Fatal("expected expired session to be invalid")
	}
	// The lazy check must also delete the entry.
	sessionMutex.RLock()
	_, still := activeSessions[token]
	sessionMutex.RUnlock()
	if still {
		t.Fatal("expected expired entry to be deleted on validation")
	}
}

func TestValidateSessionRenew(t *testing.T) {
	token := "sess-renew"
	now := time.Now()
	// Less than sessionRenewAfter remaining -> should renew. Derived from the
	// constants so this holds no matter what sessionTTL is set to.
	oldExpiry := now.Add(sessionRenewAfter / 2)
	addSessionWithTimes(token, "u1", now, oldExpiry)
	defer RemoveSession(token)

	username, renewExpiry, ok := ValidateSession(token)
	if !ok || username != "u1" {
		t.Fatalf("expected valid session for u1, got ok=%v user=%q", ok, username)
	}
	if renewExpiry.IsZero() {
		t.Fatal("expected renewExpiry to be set when < half the TTL remains")
	}
	// The stored deadline must have moved forward past the old one.
	sessionMutex.RLock()
	got := activeSessions[token].expiresAt
	sessionMutex.RUnlock()
	if !got.After(oldExpiry) {
		t.Fatalf("expected expiresAt slid forward past %v, got %v", oldExpiry, got)
	}
}

func TestValidateSessionNoRenewWhenFresh(t *testing.T) {
	token := "sess-fresh"
	now := time.Now()
	// More than sessionRenewAfter remaining -> no renewal, deadline unchanged.
	addSessionWithTimes(token, "u1", now, now.Add(sessionRenewAfter+time.Second))
	defer RemoveSession(token)

	_, renewExpiry, ok := ValidateSession(token)
	if !ok {
		t.Fatal("expected valid session")
	}
	if !renewExpiry.IsZero() {
		t.Fatal("expected no renewal when more than half the TTL remains")
	}
}

func TestValidateSessionAbsoluteCapBlocksRenew(t *testing.T) {
	token := "sess-abscap"
	now := time.Now()
	// The absolute cap sits only sessionRenewAfter/2 away, and the idle deadline
	// sits exactly at that cap (a consistent state). Renewal would want
	// now+sessionTTL but must clamp to the cap, which is not later than the
	// current deadline -> no renew. Derived from constants, TTL-agnostic.
	remaining := sessionRenewAfter / 2
	createdAt := now.Add(-sessionAbsoluteMax + remaining)
	addSessionWithTimes(token, "u1", createdAt, now.Add(remaining))
	defer RemoveSession(token)

	_, renewExpiry, ok := ValidateSession(token)
	if !ok {
		t.Fatal("expected session still valid (not yet past absolute cap)")
	}
	if !renewExpiry.IsZero() {
		t.Fatal("expected no renewal past the absolute cap")
	}
	sessionMutex.RLock()
	got := activeSessions[token].expiresAt
	sessionMutex.RUnlock()
	if got.After(createdAt.Add(sessionAbsoluteMax)) {
		t.Fatal("expiresAt must never exceed the absolute cap")
	}
}

func TestAddSessionPerUserCap(t *testing.T) {
	const user = "captest"
	RemoveSessionsForUser(user)
	defer RemoveSessionsForUser(user)

	now := time.Now()
	// Pre-fill exactly the cap with deterministically increasing createdAt so the
	// oldest is unambiguous ("cap-0"). enforceUserCapLocked runs inside AddSession.
	for i := 0; i < maxSessionsPerUser; i++ {
		tok := "cap-" + string(rune('0'+i))
		addSessionWithTimes(tok, user, now.Add(time.Duration(i)*time.Second), now.Add(sessionTTL))
	}
	// The (cap+1)-th login must evict the oldest, not be rejected (Caution 9).
	AddSession("cap-new", user)
	defer RemoveSession("cap-new")

	if _, _, ok := ValidateSession("cap-0"); ok {
		t.Fatal("expected the oldest session to be evicted at the per-user cap")
	}
	if _, _, ok := ValidateSession("cap-new"); !ok {
		t.Fatal("expected the newest login to be accepted")
	}
	// Still exactly the cap.
	count := 0
	sessionMutex.RLock()
	for _, e := range activeSessions {
		if e.username == user {
			count++
		}
	}
	sessionMutex.RUnlock()
	if count != maxSessionsPerUser {
		t.Fatalf("expected %d sessions for %q, got %d", maxSessionsPerUser, user, count)
	}
}

func TestSweepExpiredSessions(t *testing.T) {
	now := time.Now()
	addSessionWithTimes("sweep-dead", "u1", now.Add(-time.Hour), now.Add(-time.Minute))
	addSessionWithTimes("sweep-live", "u1", now, now.Add(sessionTTL))
	defer RemoveSession("sweep-dead")
	defer RemoveSession("sweep-live")

	if n := sweepExpiredSessions(); n < 1 {
		t.Fatalf("expected at least 1 session swept, got %d", n)
	}
	sessionMutex.RLock()
	_, deadStill := activeSessions["sweep-dead"]
	_, liveStill := activeSessions["sweep-live"]
	sessionMutex.RUnlock()
	if deadStill {
		t.Fatal("expected the expired session to be swept")
	}
	if !liveStill {
		t.Fatal("expected the live session to survive the sweep")
	}
}

// TestSessionAliveDoesNotSlideExpiry is the core guarantee behind the SSE
// heartbeat re-check: a live session reads as alive without its idle deadline
// moving (unlike ValidateSession, which renews). Otherwise a stream held open
// would keep renewing the session forever (plan Caution 2).
func TestSessionAliveDoesNotSlideExpiry(t *testing.T) {
	token := "alive-noslide"
	now := time.Now()
	// Deliberately near expiry (< sessionRenewAfter left) — ValidateSession would
	// renew this; SessionAlive must not.
	expiry := now.Add(sessionRenewAfter / 2)
	addSessionWithTimes(token, "u1", now, expiry)
	defer RemoveSession(token)

	if !SessionAlive(token) {
		t.Fatal("expected a live session to read as alive")
	}
	sessionMutex.RLock()
	got := activeSessions[token].expiresAt
	sessionMutex.RUnlock()
	if !got.Equal(expiry) {
		t.Fatalf("SessionAlive slid expiresAt from %v to %v", expiry, got)
	}
}

func TestSessionAliveExpiredAndMissing(t *testing.T) {
	now := time.Now()
	// Past the idle deadline.
	addSessionWithTimes("alive-expired", "u1", now.Add(-sessionTTL), now.Add(-time.Second))
	defer RemoveSession("alive-expired")
	if SessionAlive("alive-expired") {
		t.Fatal("expected an expired session to read as dead")
	}
	// Past the absolute cap even though the idle deadline is in the future.
	addSessionWithTimes("alive-abscap", "u1", now.Add(-sessionAbsoluteMax-time.Hour), now.Add(sessionTTL))
	defer RemoveSession("alive-abscap")
	if SessionAlive("alive-abscap") {
		t.Fatal("expected a session past the absolute cap to read as dead")
	}
	if SessionAlive("no-such-token") {
		t.Fatal("expected a missing token to read as dead")
	}
}

// TestAuthMiddlewareRenewsCookie drives a real request through the router to
// confirm the renewal cookie is actually written on the response.
func TestAuthMiddlewareRenewsCookie(t *testing.T) {
	handler, _ := setupTestServer(t)
	token := "mw-renew"
	now := time.Now()
	// Near expiry (< sessionRenewAfter left) so the middleware renews. "pigate"
	// exists in the seeded test DB so the middleware's DB check passes.
	addSessionWithTimes(token, "pigate", now, now.Add(sessionRenewAfter/2))
	defer RemoveSession(token)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	addSessionCookie(req, token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !hasSessionCookie(rec) {
		t.Fatal("expected the middleware to re-issue the session cookie on renewal")
	}
}

func TestAuthMiddlewareRejectsExpired(t *testing.T) {
	handler, _ := setupTestServer(t)
	token := "mw-expired"
	now := time.Now()
	addSessionWithTimes(token, "pigate", now.Add(-sessionTTL), now.Add(-time.Second))
	defer RemoveSession(token)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	addSessionCookie(req, token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for an expired session, got %d", rec.Code)
	}
}
