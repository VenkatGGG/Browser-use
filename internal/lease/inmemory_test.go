package lease

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryManagerAcquireRelease(t *testing.T) {
	manager := NewInMemoryManager()
	ctx := context.Background()

	lease1, ok, err := manager.Acquire(ctx, "node:1", "runner-1", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("acquire 1: %v", err)
	}
	if !ok {
		t.Fatalf("expected first acquire to succeed")
	}
	if lease1.Token == 0 {
		t.Fatalf("expected fencing token")
	}

	_, ok, err = manager.Acquire(ctx, "node:1", "runner-2", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("acquire 2: %v", err)
	}
	if ok {
		t.Fatalf("expected second acquire to fail while lease held")
	}

	if err := manager.Release(ctx, "node:1", "runner-1", lease1.Token); err != nil {
		t.Fatalf("release: %v", err)
	}

	lease3, ok, err := manager.Acquire(ctx, "node:1", "runner-2", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("acquire 3: %v", err)
	}
	if !ok {
		t.Fatalf("expected acquire after release to succeed")
	}
	if lease3.Token <= lease1.Token {
		t.Fatalf("expected monotonic fencing token")
	}
}

func TestInMemoryManagerLeaseExpires(t *testing.T) {
	manager := NewInMemoryManager()
	ctx := context.Background()

	_, ok, err := manager.Acquire(ctx, "node:2", "runner-1", 20*time.Millisecond)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if !ok {
		t.Fatalf("expected acquire to succeed")
	}

	time.Sleep(30 * time.Millisecond)

	_, ok, err = manager.Acquire(ctx, "node:2", "runner-2", 20*time.Millisecond)
	if err != nil {
		t.Fatalf("acquire after expiry: %v", err)
	}
	if !ok {
		t.Fatalf("expected acquire after expiry to succeed")
	}
}
