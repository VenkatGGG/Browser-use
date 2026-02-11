package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type Status string

const (
	StatusReady Status = "ready"
)

type Session struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Status    Status    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateInput struct {
	TenantID string
}

type Service interface {
	Create(ctx context.Context, input CreateInput) (Session, error)
	Delete(ctx context.Context, id string) error
}

type InMemoryService struct {
	counter atomic.Int64
	mu      sync.RWMutex
	items   map[string]Session
}

func NewInMemoryService() *InMemoryService {
	return &InMemoryService{items: make(map[string]Session)}
}

func (s *InMemoryService) Create(_ context.Context, input CreateInput) (Session, error) {
	if input.TenantID == "" {
		return Session{}, errors.New("tenant_id is required")
	}
	id := fmt.Sprintf("sess_%06d", s.counter.Add(1))
	now := time.Now().UTC()
	created := Session{
		ID:        id,
		TenantID:  input.TenantID,
		Status:    StatusReady,
		CreatedAt: now,
	}

	s.mu.Lock()
	s.items[id] = created
	s.mu.Unlock()

	return created, nil
}

func (s *InMemoryService) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.items[id]; !ok {
		return errors.New("session not found")
	}
	delete(s.items, id)
	return nil
}
