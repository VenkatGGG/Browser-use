package session

import (
	"context"
	"errors"
	"testing"
)

func TestInMemoryServiceCreateAndDelete(t *testing.T) {
	t.Parallel()

	svc := NewInMemoryService()

	created, err := svc.Create(context.Background(), CreateInput{TenantID: "tenant-a"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("expected session id")
	}
	if created.TenantID != "tenant-a" {
		t.Fatalf("unexpected tenant id %q", created.TenantID)
	}
	if created.Status != StatusReady {
		t.Fatalf("unexpected status %q", created.Status)
	}
	if created.CreatedAt.IsZero() {
		t.Fatalf("expected created_at")
	}

	found, err := svc.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("expected get id %q, got %q", created.ID, found.ID)
	}

	if err := svc.Delete(context.Background(), created.ID); err != nil {
		t.Fatalf("delete session: %v", err)
	}

	err = svc.Delete(context.Background(), created.ID)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound on second delete, got %v", err)
	}
	if _, err := svc.Get(context.Background(), created.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound on get after delete, got %v", err)
	}
}

func TestInMemoryServiceCreateRequiresTenantID(t *testing.T) {
	t.Parallel()

	svc := NewInMemoryService()
	if _, err := svc.Create(context.Background(), CreateInput{}); err == nil {
		t.Fatalf("expected tenant_id validation error")
	}
}

func TestNewPostgresServiceRequiresDSN(t *testing.T) {
	t.Parallel()

	if _, err := NewPostgresService(context.Background(), "   "); err == nil {
		t.Fatalf("expected postgres dsn validation error")
	}
}
