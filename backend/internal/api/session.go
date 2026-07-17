package api

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"
)

// In-memory session store. Deliberately NOT persisted to SQLite (SD-card
// preservation, see tech_stack_design.md §8); a process restart clears every
// session, which is an acceptable fail-safe rather than a bug.
//
// Each entry carries two deadlines: a sliding idle deadline (expiresAt, renewed
// while the session is used) and an absolute cap derived from createdAt. A token
// that leaks can therefore live at most sessionAbsoluteMax, and no longer than
// sessionTTL once the browser stops polling.
type sessionEntry struct {
	username  string
	createdAt time.Time // absolute cap anchor + picks the victim when evicting
	expiresAt time.Time // idle deadline (sliding)
}

const (
	sessionTTL         = 15 * time.Minute   // idle backstop once the browser closes/crashes (poll stops)
	sessionAbsoluteMax = 7 * 24 * time.Hour // hard cap from createdAt; a session is never renewed past this
	sessionRenewAfter  = sessionTTL / 2     // renew only when < half the TTL remains (avoids Set-Cookie churn)
	sweepInterval      = 5 * time.Minute
	maxSessionsPerUser = 5
)

var (
	sessionMutex   sync.RWMutex
	activeSessions = map[string]*sessionEntry{} // token -> entry
)

// AddSession registers a new session token for username. The signature is kept
// stable — tests create sessions through it in ~8 places — so createdAt/expiresAt
// are stamped here rather than passed in. Before inserting, the per-user cap is
// enforced by evicting that user's oldest session (Caution 9: evict, never
// reject, so an admin who forgot to log out elsewhere can't lock themselves out).
func AddSession(token, username string) {
	sessionMutex.Lock()
	defer sessionMutex.Unlock()
	enforceUserCapLocked(username)
	now := time.Now()
	activeSessions[token] = &sessionEntry{
		username:  username,
		createdAt: now,
		expiresAt: now.Add(sessionTTL),
	}
}

// enforceUserCapLocked evicts username's oldest session (by createdAt) while they
// already hold >= maxSessionsPerUser, so the caller's about-to-be-added session
// lands exactly at the cap. Must hold the write-lock.
func enforceUserCapLocked(username string) {
	for {
		count := 0
		var oldestToken string
		var oldestAt time.Time
		for token, e := range activeSessions {
			if e.username != username {
				continue
			}
			count++
			if oldestToken == "" || e.createdAt.Before(oldestAt) {
				oldestToken = token
				oldestAt = e.createdAt
			}
		}
		if count < maxSessionsPerUser {
			return
		}
		delete(activeSessions, oldestToken)
	}
}

// addSessionWithTimes injects a session with explicit timestamps. Test-only —
// lets a test create an already-expired or near-expiry session without waiting.
func addSessionWithTimes(token, username string, createdAt, expiresAt time.Time) {
	sessionMutex.Lock()
	defer sessionMutex.Unlock()
	activeSessions[token] = &sessionEntry{
		username:  username,
		createdAt: createdAt,
		expiresAt: expiresAt,
	}
}

// RemoveSession revokes a single session token (logout, or middleware rejecting
// a now-invalid user).
func RemoveSession(token string) {
	sessionMutex.Lock()
	defer sessionMutex.Unlock()
	delete(activeSessions, token)
}

// RemoveSessionsForUser purges every in-memory session belonging to a username.
// Used when an account is deleted/disabled so the lingering token can't sit in
// the map until restart. The AuthMiddleware already rejects such tokens on the
// next request (the DB is source of truth); this is just housekeeping.
func RemoveSessionsForUser(username string) {
	sessionMutex.Lock()
	defer sessionMutex.Unlock()
	for token, e := range activeSessions {
		if e.username == username {
			delete(activeSessions, token)
		}
	}
}

// ValidateSession validates a token and, in the same call, slides its idle
// deadline when it is close to expiring. It returns the bound username, a
// non-zero renewExpiry ONLY when the caller should re-issue the cookie (the
// deadline was slid), and ok=false for a missing or expired token (an expired
// entry is deleted). The common path takes only a read-lock; the rare renewal
// path upgrades to a write-lock.
func ValidateSession(token string) (username string, renewExpiry time.Time, ok bool) {
	now := time.Now()

	sessionMutex.RLock()
	e, found := activeSessions[token]
	if !found {
		sessionMutex.RUnlock()
		return "", time.Time{}, false
	}
	if now.After(e.expiresAt) {
		sessionMutex.RUnlock()
		// Expired: drop it under the write-lock, re-checking in case it was
		// renewed or removed between the two locks.
		sessionMutex.Lock()
		if cur, still := activeSessions[token]; still && now.After(cur.expiresAt) {
			delete(activeSessions, token)
		}
		sessionMutex.Unlock()
		return "", time.Time{}, false
	}
	username = e.username
	// Renew when less than half the TTL remains, unless the absolute cap is hit.
	needRenew := now.After(e.expiresAt.Add(-sessionRenewAfter)) && now.Before(e.createdAt.Add(sessionAbsoluteMax))
	sessionMutex.RUnlock()

	if !needRenew {
		return username, time.Time{}, true
	}

	sessionMutex.Lock()
	if cur, still := activeSessions[token]; still {
		newExpiry := now.Add(sessionTTL)
		if absMax := cur.createdAt.Add(sessionAbsoluteMax); newExpiry.After(absMax) {
			newExpiry = absMax // clamp to the absolute cap
		}
		if newExpiry.After(cur.expiresAt) {
			cur.expiresAt = newExpiry
			renewExpiry = newExpiry
		}
	}
	sessionMutex.Unlock()
	return username, renewExpiry, true
}

// SessionAlive reports whether a token currently maps to a live session, WITHOUT
// touching its idle deadline. This is the read-only counterpart to
// ValidateSession, meant for callers that must re-check a long-lived connection
// on a timer (the SSE log stream heartbeat) without renewing it — using
// ValidateSession there would slide the sliding-idle deadline on every heartbeat,
// so a stream left open would keep the session alive forever and defeat the
// server-side idle logout entirely (see plan Caution 2). Takes only a read-lock
// and never mutates the entry.
func SessionAlive(token string) bool {
	now := time.Now()
	sessionMutex.RLock()
	defer sessionMutex.RUnlock()
	e, found := activeSessions[token]
	if !found {
		return false
	}
	// Dead if past the idle deadline or the absolute cap.
	if now.After(e.expiresAt) || now.After(e.createdAt.Add(sessionAbsoluteMax)) {
		return false
	}
	return true
}

// setSessionCookie writes the session cookie with the canonical attributes used
// by BOTH login and mid-session renewal. This must stay the single source: a
// cookie re-issued with any differing attribute is treated by the browser as a
// separate cookie, producing duplicates (Caution 4). Secure is decided
// per-request from r.TLS so the plain-HTTP fallback mode still keeps the cookie.
func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionKey,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		Expires:  expires,
	})
}

// sweepExpiredSessions removes every entry past its idle deadline and returns how
// many were reaped. The lazy check in ValidateSession only drops tokens that are
// hit again; an abandoned token would otherwise sit in the map until restart, so
// the sweeper reaps them on a timer. Split out from the goroutine so tests can
// call it directly.
func sweepExpiredSessions() int {
	now := time.Now()
	sessionMutex.Lock()
	defer sessionMutex.Unlock()
	removed := 0
	for token, e := range activeSessions {
		if now.After(e.expiresAt) {
			delete(activeSessions, token)
			removed++
		}
	}
	return removed
}

// StartSessionSweeper runs sweepExpiredSessions on a ticker until ctx is done.
// Pattern mirrors service/event_log.go's Start(ctx).
func StartSessionSweeper(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(sweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if n := sweepExpiredSessions(); n > 0 {
					log.Printf("[Session] Swept %d expired session(s)", n)
				}
			}
		}
	}()
}
