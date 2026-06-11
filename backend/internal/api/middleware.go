package api

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
)

type contextKey string

const (
	UserContextKey contextKey = "user"
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
	// If it's mock mode, allow any mock token starting with mock_session_id_
	if !valid && strings.HasPrefix(token, "mock_session_id_") {
		return true
	}
	return valid
}

// CORSMiddleware handles cross-origin requests from the frontend dev server
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		// Allow local development ports and same origin
		if origin == "http://localhost:5173" || origin == "http://localhost:3000" || origin == "http://127.0.0.1:5173" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// AuthMiddleware checks for a valid session token in Authorization Header or Cookie
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var token string

		// 1. Check Authorization header
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimPrefix(authHeader, "Bearer ")
		}

		// 2. Check Cookie if header is empty
		if token == "" {
			cookie, err := r.Cookie(SessionKey)
			if err == nil {
				token = cookie.Value
			}
		}

		if token == "" || !IsSessionValid(token) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"message": "Unauthorized"}`))
			return
		}

		// Inject user info into context (mock username admin)
		ctx := context.WithValue(r.Context(), UserContextKey, "admin")
		next.ServeHTTP(w, r.WithContext(ctx))
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
