package task

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
	StatusQueued Status = "queued"
)

type Task struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	URL       string    `json:"url"`
	Goal      string    `json:"goal"`
	Status    Status    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateInput struct {
	SessionID string
	URL       string
	Goal      string
}

type Service interface {
	Create(ctx context.Context, input CreateInput) (Task, error)
	Get(ctx context.Context, id string) (Task, error)
}

type InMemoryService struct {
	counter atomic.Int64
	mu      sync.RWMutex
	items   map[string]Task
}

func NewInMemoryService() *InMemoryService {
	return &InMemoryService{items: make(map[string]Task)}
}

func (s *InMemoryService) Create(_ context.Context, input CreateInput) (Task, error) {
	if input.SessionID == "" {
		return Task{}, errors.New("session_id is required")
	}
	if input.URL == "" {
		return Task{}, errors.New("url is required")
	}
	if input.Goal == "" {
		return Task{}, errors.New("goal is required")
	}
	id := fmt.Sprintf("task_%06d", s.counter.Add(1))
	now := time.Now().UTC()
	created := Task{
		ID:        id,
		SessionID: input.SessionID,
		URL:       input.URL,
		Goal:      input.Goal,
		Status:    StatusQueued,
		CreatedAt: now,
	}

	s.mu.Lock()
	s.items[id] = created
	s.mu.Unlock()

	return created, nil
}

func (s *InMemoryService) Get(_ context.Context, id string) (Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	found, ok := s.items[id]
	if !ok {
		return Task{}, errors.New("task not found")
	}
	return found, nil
}
