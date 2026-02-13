package idempotency

import (
	"context"
	"time"
)

type Entry struct {
	StatusCode  int                 `json:"status_code"`
	ContentType string              `json:"content_type"`
	Headers     map[string][]string `json:"headers,omitempty"`
	Body        []byte              `json:"body"`
}

type Store interface {
	Get(ctx context.Context, scope, key string) (Entry, bool, error)
	Claim(ctx context.Context, scope, key, owner string, ttl time.Duration) (bool, error)
	Save(ctx context.Context, scope, key string, entry Entry, ttl time.Duration) error
	Release(ctx context.Context, scope, key, owner string) error
}
