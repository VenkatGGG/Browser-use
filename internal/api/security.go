package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/VenkatGGG/Browser-use/pkg/httpx"
)

func (s *Server) withAPISecurity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !requiresCreateAuthAndRateLimit(r) {
			next.ServeHTTP(w, r)
			return
		}

		if strings.TrimSpace(s.requiredAPIKey) != "" && !requestHasAPIKey(r, s.requiredAPIKey) {
			httpx.WriteError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid api key")
			return
		}

		if s.rateLimiter != nil {
			clientKey := requestClientIdentity(r)
			if !s.rateLimiter.Allow(clientKey, time.Now().UTC()) {
				httpx.WriteError(w, http.StatusTooManyRequests, "rate_limited", "request rate limit exceeded")
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func requiresCreateAuthAndRateLimit(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	path := strings.TrimSpace(r.URL.Path)
	switch path {
	case "/sessions", "/v1/sessions", "/task", "/v1/tasks":
		return true
	default:
		return strings.HasPrefix(path, "/v1/tasks/") && strings.HasSuffix(path, "/replay")
	}
}

func requestHasAPIKey(r *http.Request, expected string) bool {
	want := strings.TrimSpace(expected)
	if want == "" {
		return true
	}
	candidates := []string{
		strings.TrimSpace(r.Header.Get("X-API-Key")),
		strings.TrimSpace(r.Header.Get("x-api-key")),
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		candidates = append(candidates, strings.TrimSpace(auth[7:]))
	}

	for _, candidate := range candidates {
		if candidate == want {
			return true
		}
	}
	return false
}

func requestClientIdentity(r *http.Request) string {
	forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if forwarded != "" {
		first := strings.TrimSpace(strings.Split(forwarded, ",")[0])
		if first != "" {
			return first
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	raw := strings.TrimSpace(r.RemoteAddr)
	if raw != "" {
		return raw
	}
	return "unknown"
}

type fixedWindowLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	clients map[string]rateBucket
}

type rateBucket struct {
	windowStart time.Time
	count       int
}

func newFixedWindowLimiter(limit int, window time.Duration) *fixedWindowLimiter {
	if limit <= 0 {
		limit = 1
	}
	if window <= 0 {
		window = time.Minute
	}
	return &fixedWindowLimiter{
		limit:   limit,
		window:  window,
		clients: make(map[string]rateBucket),
	}
}

func (l *fixedWindowLimiter) Allow(client string, now time.Time) bool {
	key := strings.TrimSpace(client)
	if key == "" {
		key = "unknown"
	}
	t := now.UTC()
	windowStart := t.Truncate(l.window)

	l.mu.Lock()
	defer l.mu.Unlock()

	bucket := l.clients[key]
	if bucket.windowStart.IsZero() || !bucket.windowStart.Equal(windowStart) {
		bucket = rateBucket{
			windowStart: windowStart,
			count:       0,
		}
	}
	if bucket.count >= l.limit {
		return false
	}
	bucket.count++
	l.clients[key] = bucket
	l.pruneLocked(windowStart)
	return true
}

func (l *fixedWindowLimiter) pruneLocked(activeWindowStart time.Time) {
	// Keep map bounded during long runs.
	if len(l.clients) < 1000 {
		return
	}
	cutoff := activeWindowStart.Add(-2 * l.window)
	for key, bucket := range l.clients {
		if bucket.windowStart.Before(cutoff) {
			delete(l.clients, key)
		}
	}
}
