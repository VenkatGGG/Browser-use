package idempotency

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryStoreClaimSaveGetRelease(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()

	claimed, err := store.Claim(ctx, "sessions:create", "abc", "owner-1", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if !claimed {
		t.Fatalf("expected initial claim")
	}

	claimed, err = store.Claim(ctx, "sessions:create", "abc", "owner-2", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if claimed {
		t.Fatalf("expected second claim to fail while lock held")
	}

	entry := Entry{
		StatusCode:  201,
		ContentType: "application/json",
		Body:        []byte(`{"id":"sess_1"}`),
	}
	if err := store.Save(ctx, "sessions:create", "abc", entry, time.Minute); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, ok, err := store.Get(ctx, "sessions:create", "abc")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !ok {
		t.Fatalf("expected cached entry")
	}
	if got.StatusCode != 201 || string(got.Body) != `{"id":"sess_1"}` {
		t.Fatalf("unexpected cached entry: %#v", got)
	}

	if err := store.Release(ctx, "sessions:create", "abc", "owner-1"); err != nil {
		t.Fatalf("release: %v", err)
	}
	claimed, err = store.Claim(ctx, "sessions:create", "abc", "owner-2", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("claim after release: %v", err)
	}
	if !claimed {
		t.Fatalf("expected claim after release")
	}
}
