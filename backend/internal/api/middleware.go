package api

import (
	"context"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"pigate/internal/model"
)

type contextKey string

const (
	UserContextKey contextKey = "user"
	RoleContextKey contextKey = "role"
	SessionKey     string     = "pigate_session"
)

// The in-memory session store (AddSession/RemoveSession/ValidateSession/…) lives
// in session.go alongside the TTL/renewal/sweeper logic.

// devCORSOrigins is the allow-list of frontend dev-server origins whose
// credentialed cross-origin requests are echoed back — ONLY when the binary is
// started with -allow-dev-cors. A production build serves the SPA same-origin
// and never needs these, so the default (gate off) keeps a production binary
// from echoing any cross-origin Origin at all.
var devCORSOrigins = map[string]bool{
	"http://localhost:5173": true,
	"http://localhost:3000": true,
	"http://127.0.0.1:5173": true,
}

// CORSMiddleware handles cross-origin requests from the frontend dev server.
// It is a method so it can consult s.allowDevCORS: the dev-origin echo (which
// enables credentialed cross-origin XHR from `yarn dev`) is only active when
// the operator opted in via -allow-dev-cors. Without the flag, no dev origin is
// echoed (production is same-origin and unaffected).
func (s *Server) CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if s.allowDevCORS && devCORSOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Requested-With")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// AuthMiddleware checks for a valid session token in the HttpOnly cookie.
// The Authorization: Bearer path was removed — the cookie is the single
// channel for the session token (cookie-only auth).
func (s *Server) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var token string
		if cookie, err := r.Cookie(SessionKey); err == nil {
			token = cookie.Value
		}

		// Validate + (rarely) renew in one call. renewExpiry is non-zero only when
		// the sliding idle deadline was pushed forward and the cookie must be
		// re-issued; ok is false for a missing or server-side-expired token.
		username, renewExpiry, ok := ValidateSession(token)
		if token == "" || !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"message": "Unauthorized"}`))
			return
		}

		// Re-issue the cookie with the slid deadline. This MUST happen before
		// next.ServeHTTP — once the handler calls WriteHeader the header is
		// silently dropped and the browser cookie expires ahead of the server
		// session (Caution 3).
		if !renewExpiry.IsZero() {
			setSessionCookie(w, r, token, renewExpiry)
		}

		// Query the DB every request (source of truth). This makes deleting,
		// disabling, or demoting a user take effect immediately on any lingering
		// session — SQLite in-process is cheap and we already query here anyway.
		user, err := s.repo.GetUserByUsername(username)
		if err != nil || user == nil || user.Status == model.StatusDisabled {
			RemoveSession(token)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"message": "บัญชีนี้ถูกปิดใช้งานหรือไม่มีอยู่แล้ว"}`))
			return
		}

		// Inject the real username + role into context for downstream handlers
		// and RoleReadOnlyMiddleware.
		ctx := context.WithValue(r.Context(), UserContextKey, user.Username)
		ctx = context.WithValue(ctx, RoleContextKey, user.Role)

		// Force password change for THIS user (not a hardcoded account) when
		// is_initial is set — except for the change-password, logout, and
		// session-check endpoints so the flow can complete.
		if user.IsInitial {
			if r.URL.Path != "/api/system/password" && r.URL.Path != "/api/auth/logout" && r.URL.Path != "/api/auth/session" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"message": "Change password required", "mustChangePassword": true}`))
				return
			}
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RoleReadOnlyMiddleware blocks every mutating request (POST/PUT/PATCH/DELETE)
// when the authenticated user is not a super_admin. It is fail-closed: any role
// that is missing/unknown is treated as read-only. A short allow-list lets a
// read-only user still log out and change their own password. Must run AFTER
// AuthMiddleware (it reads the role from context).
func RoleReadOnlyMiddleware(next http.Handler) http.Handler {
	// Exact-path allow-list (no prefix matching) for read-only users.
	readOnlyAllowed := map[string]bool{
		"/api/auth/logout":     true,
		"/api/system/password": true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role, _ := r.Context().Value(RoleContextKey).(string)
		// Fail-closed: only an explicit super_admin may mutate.
		if role != model.RoleSuperAdmin {
			if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete || r.Method == http.MethodPatch {
				if !readOnlyAllowed[r.URL.Path] {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusForbidden)
					w.Write([]byte(`{"message": "สิทธิ์ของคุณเป็นแบบอ่านอย่างเดียว ไม่สามารถแก้ไขได้"}`))
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// SuperAdminMiddleware restricts a route to super_admin only, including GET
// requests (used for /api/users/* so a read-only admin can't even list
// accounts). Must run AFTER AuthMiddleware.
func SuperAdminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role, _ := r.Context().Value(RoleContextKey).(string)
		if role != model.RoleSuperAdmin {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"message": "เฉพาะ super_admin เท่านั้นที่เข้าถึงส่วนนี้ได้"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Simple Token Bucket Rate Limiter
type rateLimiter struct {
	tokens   int
	max      int
	last     time.Time // refill clock (only advanced when a token is actually added)
	lastSeen time.Time // idle-eviction clock (advanced on every allow() call)
	mu       sync.Mutex
}

const (
	// A full bucket refills in 10s (5 tokens × 2s); 10 min idle means the entry
	// is unquestionably stale — evicting it only resets the bucket to full.
	limiterIdleAfter     = 10 * time.Minute
	limiterSweepInterval = 10 * time.Minute
	// Backstop between sweeps so a burst of unique source IPs (e.g. IPv6 privacy
	// address cycling) can't grow the map without bound. ~4096 buckets is a few
	// hundred KB.
	maxLimiterEntries = 4096
	// Minimum gap between the synchronous at-cap sweeps in getLimiter. Under a
	// sustained unique-IP flood the map stays full of non-idle buckets, and
	// re-scanning all ~4096 entries (locking each one) on EVERY insert while
	// holding limitersMu would serialize all login traffic behind O(n) lock work
	// exactly while under attack — so a fruitless sweep is not retried for a
	// minute and inserts fall through to the O(1) random eviction instead.
	limiterCapSweepMinGap = time.Minute
)

var (
	limitersMu sync.Mutex
	limiters   = make(map[string]*rateLimiter)
	// lastCapSweep records when getLimiter last ran its at-cap sweep (see
	// limiterCapSweepMinGap). Guarded by limitersMu.
	lastCapSweep time.Time
)

func getLimiter(ip string) *rateLimiter {
	limitersMu.Lock()
	defer limitersMu.Unlock()

	lim, exists := limiters[ip]
	if !exists {
		now := time.Now()
		// Enforce the hard cap before inserting. Sweep idle entries first (rate-
		// limited by limiterCapSweepMinGap); if the map is still full every bucket
		// is active (only reachable under attack), so drop one arbitrary entry
		// (Go map iteration order) to make room. A dropped bucket just resets to
		// full on next use — it never locks anyone out.
		if len(limiters) >= maxLimiterEntries {
			if now.Sub(lastCapSweep) >= limiterCapSweepMinGap {
				lastCapSweep = now
				evictIdleLimitersLocked(now)
			}
			if len(limiters) >= maxLimiterEntries {
				for k := range limiters {
					delete(limiters, k)
					break
				}
			}
		}
		lim = &rateLimiter{
			tokens:   5,
			max:      5,
			last:     now,
			lastSeen: now,
		}
		limiters[ip] = lim
	}
	return lim
}

func (lim *rateLimiter) allow() bool {
	lim.mu.Lock()
	defer lim.mu.Unlock()

	now := time.Now()
	// lastSeen tracks activity for eviction and must advance on EVERY call —
	// unlike `last`, which only moves when a token is refilled. Reusing `last`
	// for idle detection would let a steady sub-2s burst look "idle" and get its
	// bucket evicted (and reset to full) mid-attack. See rate-limiter-eviction plan.
	lim.lastSeen = now

	// Add 1 token per 2 seconds
	elapsed := now.Sub(lim.last)
	refill := int(elapsed / (2 * time.Second))
	if refill > 0 {
		lim.tokens += refill
		if lim.tokens > lim.max {
			lim.tokens = lim.max
		}
		lim.last = now
	}

	if lim.tokens > 0 {
		lim.tokens--
		return true
	}
	return false
}

// evictIdleLimitersLocked deletes entries idle longer than limiterIdleAfter.
// Caller MUST hold limitersMu. Reading lastSeen takes each entry's own mutex —
// lock order is always limitersMu → lim.mu (allow() never takes limitersMu),
// so there is no inversion.
func evictIdleLimitersLocked(now time.Time) int {
	removed := 0
	for ip, lim := range limiters {
		lim.mu.Lock()
		idle := now.Sub(lim.lastSeen) > limiterIdleAfter
		lim.mu.Unlock()
		if idle {
			delete(limiters, ip)
			removed++
		}
	}
	return removed
}

// sweepIdleLimiters evicts stale entries under the map lock. Split out so tests
// can call it directly.
func sweepIdleLimiters() int {
	limitersMu.Lock()
	defer limitersMu.Unlock()
	return evictIdleLimitersLocked(time.Now())
}

// StartLimiterSweeper reaps idle rate-limiter buckets on a ticker until ctx is
// done. Pattern mirrors StartSessionSweeper / service/event_log.go's Start(ctx).
func StartLimiterSweeper(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(limiterSweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if n := sweepIdleLimiters(); n > 0 {
					log.Printf("[RateLimit] Swept %d idle limiter entry(ies)", n)
				}
			}
		}
	}()
}

// RateLimitMiddleware limits access to login APIs to prevent brute force
func RateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Key by source IP. SplitHostPort strips the port cleanly for both IPv4
		// and bracketed IPv6; fall back to the raw RemoteAddr if it has no port.
		ip := r.RemoteAddr
		if host, _, err := net.SplitHostPort(ip); err == nil {
			ip = host
		}

		lim := getLimiter(ip)
		if !lim.allow() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"message": "Too many requests. Please wait."}`))
			return
		}

		next.ServeHTTP(w, r)
	})
}

// DisableEditMiddleware blocks all POST, PUT, DELETE, and PATCH requests except for auth login/logout
func DisableEditMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete || r.Method == http.MethodPatch {
			if r.URL.Path != "/api/auth/login" && r.URL.Path != "/api/auth/logout" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"message": "Editing is disabled (Read-only mode)"}`))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// maxRequestBody caps request bodies for every endpoint except config import
// (which sets its own 10 MB cap). Every JSON config the API accepts is a few KB;
// 1 MB is generous headroom that still stops a body from ballooning the Pi's RAM.
const maxRequestBody = 1 << 20 // 1 MB

// bodyLimitExemptPaths lists endpoints that manage their own (larger) body cap.
// config/import handles multi-MB backups and caps at 10 MB itself — wrapping it
// here with the smaller 1 MB reader would win (the innermost MaxBytesReader is
// the effective limit) and break large imports. Any future large-upload endpoint
// must be added here too.
var bodyLimitExemptPaths = map[string]bool{
	"/api/system/config/import": true,
}

// BodyLimitMiddleware wraps r.Body in a MaxBytesReader so oversized payloads
// fail at read/decode time (the handler returns its normal 4xx) instead of being
// buffered into memory. Mounted innermost (closest to the mux) so the exempt
// import path can still install its own larger cap.
func BodyLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && !bodyLimitExemptPaths[r.URL.Path] {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		}
		next.ServeHTTP(w, r)
	})
}

// cspPolicy is the Content-Security-Policy served on every response.
//   - script-src 'self' is strict: the built index.html has no inline script, so
//     an injected <script> or inline handler is blocked outright (primary XSS wall).
//   - style-src needs 'unsafe-inline' — recharts, the shadcn chart component
//     (chart.tsx injects a <style> block), and swagger-ui all emit inline styles
//     we don't control. CSS injection risk is far lower than script.
//   - frame-ancestors 'none' (+ X-Frame-Options below) blocks clickjacking.
//
// Deliberately NO Strict-Transport-Security: PiGate falls back to plain HTTP when
// TLS setup fails (main.go), and a browser that cached HSTS would then refuse the
// fallback and lock the admin out of the box. Do not add HSTS here.
//
// If the frontend ever loads an external resource (font/CDN/remote image/new
// websocket), it will fail silently in production only (dev via Vite has no CSP) —
// the single place to update is this constant.
const cspPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; " +
	"font-src 'self' data:; " +
	"connect-src 'self'; " +
	"frame-ancestors 'none'; " +
	"base-uri 'self'; form-action 'self'; object-src 'none'"

// SecurityHeadersMiddleware sets fixed hardening headers on every response
// (static SPA, API JSON, SSE, and error responses from inner middleware alike).
// Uses Set (not Add) so a duplicate header can never produce a multi-value CSP
// that browsers intersect into an over-strict, page-breaking policy.
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", cspPolicy)
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
