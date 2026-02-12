package main

import (
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
