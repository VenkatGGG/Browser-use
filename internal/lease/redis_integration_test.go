package lease

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func TestRedisManagerRenewExtendsLeaseWhenOwnerMatches(t *testing.T) {
	client := newRedisTestClient(t)
	ctx := context.Background()
	manager := NewRedisManager(client, "browseruse:test:"+uuid.NewString())

	initial, ok, err := manager.Acquire(ctx, "node:1", "runner-1", 180*time.Millisecond)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if !ok {
		t.Fatalf("expected acquire to succeed")
	}

	time.Sleep(90 * time.Millisecond)
	renewed, ok, err := manager.Renew(ctx, "node:1", "runner-1", initial.Token, 260*time.Millisecond)
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if !ok {
		t.Fatalf("expected renew to succeed")
	}
	if !renewed.ExpiresAt.After(initial.ExpiresAt) {
		t.Fatalf("expected renewed expiry to extend beyond initial expiry")
	}

	time.Sleep(140 * time.Millisecond)
	_, ok, err = manager.Acquire(ctx, "node:1", "runner-2", 120*time.Millisecond)
	if err != nil {
		t.Fatalf("acquire while renewed lease is active: %v", err)
	}
	if ok {
		t.Fatalf("expected lease to remain held after renew")
	}
}

func TestRedisManagerRenewFailsWhenOwnerTokenMismatch(t *testing.T) {
	client := newRedisTestClient(t)
	ctx := context.Background()
	manager := NewRedisManager(client, "browseruse:test:"+uuid.NewString())

	initial, ok, err := manager.Acquire(ctx, "node:2", "runner-1", 300*time.Millisecond)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if !ok {
		t.Fatalf("expected acquire to succeed")
	}

	_, ok, err = manager.Renew(ctx, "node:2", "runner-2", initial.Token, 300*time.Millisecond)
	if err != nil {
		t.Fatalf("renew with mismatched owner should not error: %v", err)
	}
	if ok {
		t.Fatalf("expected renew to fail for mismatched owner")
	}
}

func newRedisTestClient(t *testing.T) *redis.Client {
	t.Helper()

	addr := strings.TrimSpace(os.Getenv("TEST_REDIS_ADDR"))
	if addr == "" {
		t.Skip("set TEST_REDIS_ADDR to run redis integration tests")
	}

	client := redis.NewClient(&redis.Options{
		Addr: addr,
		DB:   15,
	})
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("redis not reachable at TEST_REDIS_ADDR=%s: %v", addr, err)
	}
	if err := client.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("flush redis test db: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
	})
	return client
}
