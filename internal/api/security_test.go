package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequestHasAPIKeySupportsBearerAuthorization(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/v1/tasks", nil)
	req.Header.Set("Authorization", "Bearer topsecret")
	if !requestHasAPIKey(req, "topsecret") {
		t.Fatalf("expected bearer token to satisfy api key check")
	}
}

func TestRequestClientIdentityPrefersXForwardedForFirstIP(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/v1/tasks", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.10, 10.0.0.5")
	req.RemoteAddr = "127.0.0.1:12345"

	got := requestClientIdentity(req)
	if got != "203.0.113.10" {
		t.Fatalf("expected first forwarded ip, got %q", got)
	}
}

func TestRequiresCreateAuthAndRateLimitMatchesReplayAndCancelPosts(t *testing.T) {
	t.Parallel()

	postReplay := httptest.NewRequest(http.MethodPost, "/v1/tasks/task_1/replay", nil)
	if !requiresCreateAuthAndRateLimit(postReplay) {
		t.Fatalf("expected replay post route to require auth/rate limit")
	}

	getReplay := httptest.NewRequest(http.MethodGet, "/v1/tasks/task_1/replay", nil)
	if requiresCreateAuthAndRateLimit(getReplay) {
		t.Fatalf("did not expect get replay route to require auth/rate limit")
	}

	postByID := httptest.NewRequest(http.MethodPost, "/v1/tasks/task_1", nil)
	if requiresCreateAuthAndRateLimit(postByID) {
		t.Fatalf("did not expect generic task by-id post route to require auth/rate limit")
	}

	postCancel := httptest.NewRequest(http.MethodPost, "/v1/tasks/task_1/cancel", nil)
	if !requiresCreateAuthAndRateLimit(postCancel) {
		t.Fatalf("expected cancel post route to require auth/rate limit")
	}
}

func TestFixedWindowLimiterResetsAcrossWindows(t *testing.T) {
	t.Parallel()

	limiter := newFixedWindowLimiter(1, time.Minute)
	clientKey := "198.51.100.4"
	windowStart := time.Date(2026, time.February, 12, 10, 0, 0, 0, time.UTC)

	if !limiter.Allow(clientKey, windowStart.Add(10*time.Second)) {
		t.Fatalf("expected first request in window to be allowed")
	}
	if limiter.Allow(clientKey, windowStart.Add(20*time.Second)) {
		t.Fatalf("expected second request in same window to be denied")
	}
	if !limiter.Allow(clientKey, windowStart.Add(70*time.Second)) {
		t.Fatalf("expected request in next window to be allowed")
	}
}
