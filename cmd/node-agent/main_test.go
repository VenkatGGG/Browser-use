package main

import (
	"context"
	"testing"
	"time"
)

func TestActionTimeoutBounds(t *testing.T) {
	t.Parallel()

	if got := actionTimeout(0); got != 12*time.Second {
		t.Fatalf("expected default timeout 12s, got %s", got)
	}
	if got := actionTimeout(1); got != 250*time.Millisecond {
		t.Fatalf("expected minimum timeout 250ms, got %s", got)
	}
	if got := actionTimeout(65000); got != 60*time.Second {
		t.Fatalf("expected maximum timeout 60s, got %s", got)
	}
	if got := actionTimeout(1800); got != 1800*time.Millisecond {
		t.Fatalf("expected passthrough timeout 1800ms, got %s", got)
	}
}

func TestBoundedActionDelay(t *testing.T) {
	t.Parallel()

	if got := boundedActionDelay(0, 500*time.Millisecond); got != 500*time.Millisecond {
		t.Fatalf("expected fallback delay, got %s", got)
	}
	if got := boundedActionDelay(200, 500*time.Millisecond); got != 200*time.Millisecond {
		t.Fatalf("expected passthrough delay 200ms, got %s", got)
	}
	if got := boundedActionDelay(30000, 500*time.Millisecond); got != 15*time.Second {
		t.Fatalf("expected capped delay 15s, got %s", got)
	}
}

func TestValidateEgressTarget(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	if err := validateEgressTarget(ctx, "https://example.com", "open", nil); err != nil {
		t.Fatalf("expected open mode to allow public host: %v", err)
	}
	if err := validateEgressTarget(ctx, "https://127.0.0.1", "public_only", nil); err == nil {
		t.Fatalf("expected public_only mode to block private literal ip")
	}
	if err := validateEgressTarget(ctx, "https://example.com", "deny_all", nil); err == nil {
		t.Fatalf("expected deny_all mode to block egress")
	}
	if err := validateEgressTarget(ctx, "https://api.example.com/path", "open", []string{"*.example.com"}); err != nil {
		t.Fatalf("expected allowlist wildcard to permit host: %v", err)
	}
	if err := validateEgressTarget(ctx, "https://google.com", "open", []string{"example.com"}); err == nil {
		t.Fatalf("expected allowlist to block unmatched host")
	}
}
