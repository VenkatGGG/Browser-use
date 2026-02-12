package lease

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

type inMemoryEntry struct {
	owner     string
	token     uint64
	expiresAt time.Time
}

type InMemoryManager struct {
	mu      sync.Mutex
	seq     uint64
	entries map[string]inMemoryEntry
}

func NewInMemoryManager() *InMemoryManager {
	return &InMemoryManager{
		entries: make(map[string]inMemoryEntry),
	}
}

func (m *InMemoryManager) Acquire(_ context.Context, resource, owner string, ttl time.Duration) (Lease, bool, error) {
	resource = strings.TrimSpace(resource)
	owner = strings.TrimSpace(owner)
	if resource == "" {
		return Lease{}, false, errors.New("resource is required")
	}
	if owner == "" {
		return Lease{}, false, errors.New("owner is required")
	}
	if ttl <= 0 {
		ttl = 90 * time.Second
	}

	now := time.Now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok := m.entries[resource]; ok && now.Before(existing.expiresAt) {
		return Lease{}, false, nil
	}

	m.seq++
	lease := Lease{
		Token:     m.seq,
		ExpiresAt: now.Add(ttl),
	}
	m.entries[resource] = inMemoryEntry{
		owner:     owner,
		token:     lease.Token,
		expiresAt: lease.ExpiresAt,
	}
	return lease, true, nil
}

func (m *InMemoryManager) Release(_ context.Context, resource, owner string, token uint64) error {
	resource = strings.TrimSpace(resource)
	owner = strings.TrimSpace(owner)
	if resource == "" {
		return errors.New("resource is required")
	}
	if owner == "" {
		return errors.New("owner is required")
	}
	if token == 0 {
		return errors.New("token is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.entries[resource]
	if !ok {
		return nil
	}
	if existing.owner != owner || existing.token != token {
		return nil
	}
	delete(m.entries, resource)
	return nil
}

func (m *InMemoryManager) Renew(_ context.Context, resource, owner string, token uint64, ttl time.Duration) (Lease, bool, error) {
	resource = strings.TrimSpace(resource)
	owner = strings.TrimSpace(owner)
	if resource == "" {
		return Lease{}, false, errors.New("resource is required")
	}
	if owner == "" {
		return Lease{}, false, errors.New("owner is required")
	}
	if token == 0 {
		return Lease{}, false, errors.New("token is required")
	}
	if ttl <= 0 {
		ttl = 90 * time.Second
	}

	now := time.Now().UTC()

	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.entries[resource]
	if !ok {
		return Lease{}, false, nil
	}
	if now.After(existing.expiresAt) {
		delete(m.entries, resource)
		return Lease{}, false, nil
	}
	if existing.owner != owner || existing.token != token {
		return Lease{}, false, nil
	}

	next := now.Add(ttl)
	existing.expiresAt = next
	m.entries[resource] = existing
	return Lease{
		Token:     token,
		ExpiresAt: next,
	}, true, nil
}
