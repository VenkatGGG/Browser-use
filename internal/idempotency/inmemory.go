package idempotency

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"
)

type memoryItem struct {
	entry     Entry
	expiresAt time.Time
}

type claimItem struct {
	owner     string
	expiresAt time.Time
}

type InMemoryStore struct {
	mu     sync.Mutex
	items  map[string]memoryItem
	claims map[string]claimItem
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		items:  make(map[string]memoryItem),
		claims: make(map[string]claimItem),
	}
}

func (s *InMemoryStore) Get(_ context.Context, scope, key string) (Entry, bool, error) {
	compound, err := normalizeCompoundKey(scope, key)
	if err != nil {
		return Entry{}, false, err
	}

	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.items[compound]
	if !ok {
		return Entry{}, false, nil
	}
	if !item.expiresAt.IsZero() && now.After(item.expiresAt) {
		delete(s.items, compound)
		return Entry{}, false, nil
	}
	entry := item.entry
	entry.Body = append([]byte(nil), entry.Body...)
	entry.Headers = cloneHeaders(entry.Headers)
	return entry, true, nil
}

func (s *InMemoryStore) Claim(_ context.Context, scope, key, owner string, ttl time.Duration) (bool, error) {
	compound, err := normalizeCompoundKey(scope, key)
	if err != nil {
		return false, err
	}
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return false, errors.New("owner is required")
	}
	if ttl <= 0 {
		ttl = 30 * time.Second
	}

	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.claims[compound]
	if ok && now.Before(existing.expiresAt) {
		return false, nil
	}
	s.claims[compound] = claimItem{
		owner:     owner,
		expiresAt: now.Add(ttl),
	}
	return true, nil
}

func (s *InMemoryStore) Save(_ context.Context, scope, key string, entry Entry, ttl time.Duration) error {
	compound, err := normalizeCompoundKey(scope, key)
	if err != nil {
		return err
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	cloned := entry
	cloned.Body = append([]byte(nil), entry.Body...)
	cloned.Headers = cloneHeaders(entry.Headers)
	s.items[compound] = memoryItem{
		entry:     cloned,
		expiresAt: now.Add(ttl),
	}
	return nil
}

func cloneHeaders(src map[string][]string) map[string][]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string][]string, len(src))
	for key, values := range src {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func (s *InMemoryStore) Release(_ context.Context, scope, key, owner string) error {
	compound, err := normalizeCompoundKey(scope, key)
	if err != nil {
		return err
	}
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return errors.New("owner is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.claims[compound]
	if !ok {
		return nil
	}
	if existing.owner != owner {
		return nil
	}
	delete(s.claims, compound)
	return nil
}

func normalizeCompoundKey(scope, key string) (string, error) {
	scope = strings.TrimSpace(scope)
	key = strings.TrimSpace(key)
	if scope == "" {
		return "", errors.New("scope is required")
	}
	if key == "" {
		return "", errors.New("key is required")
	}
	sum := sha256.Sum256([]byte(scope + "|" + key))
	return scope + ":" + hex.EncodeToString(sum[:]), nil
}
