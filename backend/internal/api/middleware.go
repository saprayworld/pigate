package api

import (
	"context"
	"net/http"
	"strings"
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

// Simple in-memory session store
var (
	sessionMutex   sync.RWMutex
	activeSessions = map[string]string{} // token -> username
)

func AddSession(token, username string) {
	sessionMutex.Lock()
	defer sessionMutex.Unlock()
	activeSessions[token] = username
}

func RemoveSession(token string) {
	sessionMutex.Lock()
	defer sessionMutex.Unlock()
	delete(activeSessions, token)
}

func IsSessionValid(token string) bool {
	sessionMutex.RLock()
	defer sessionMutex.RUnlock()
	_, valid := activeSessions[token]
	return valid
}

// GetUsernameByToken resolves the username bound to a session token.
func GetUsernameByToken(token string) (string, bool) {
	sessionMutex.RLock()
	defer sessionMutex.RUnlock()
	username, ok := activeSessions[token]
	return username, ok
}

// RemoveSessionsForUser purges every in-memory session belonging to a username.
// Used when an account is deleted/disabled so the lingering token can't sit in
// the map until restart. The AuthMiddleware already rejects such tokens on the
// next request (the DB is source of truth); this is just housekeeping.
func RemoveSessionsForUser(username string) {
	sessionMutex.Lock()
	defer sessionMutex.Unlock()
	for token, u := range activeSessions {
		if u == username {
			delete(activeSessions, token)
		}
	}
}

// CORSMiddleware handles cross-origin requests from the frontend dev server
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		// Allow local development ports and same origin
		if origin == "http://localhost:5173" || origin == "http://localhost:3000" || origin == "http://127.0.0.1:5173" {
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

		if token == "" || !IsSessionValid(token) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"message": "Unauthorized"}`))
			return
		}

		// Resolve the real user bound to this session token.
		username, ok := GetUsernameByToken(token)
		if !ok || username == "" {
			RemoveSession(token)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"message": "Unauthorized"}`))
			return
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
	tokens int
	max    int
	last   time.Time
	mu     sync.Mutex
}

var (
	limitersMu sync.Mutex
	limiters   = make(map[string]*rateLimiter)
)

func getLimiter(ip string) *rateLimiter {
	limitersMu.Lock()
	defer limitersMu.Unlock()

	lim, exists := limiters[ip]
	if !exists {
		lim = &rateLimiter{
			tokens: 5,
			max:    5,
			last:   time.Now(),
		}
		limiters[ip] = lim
	}
	return lim
}

func (lim *rateLimiter) allow() bool {
	lim.mu.Lock()
	defer lim.mu.Unlock()

	now := time.Now()
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

// RateLimitMiddleware limits access to login APIs to prevent brute force
func RateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Use RemoteAddr as key (simplistic IP rate limit)
		ip := r.RemoteAddr
		if idx := strings.LastIndex(ip, ":"); idx != -1 {
			ip = ip[:idx]
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
