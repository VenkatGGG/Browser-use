package lease

import (
	"context"
	"time"
)

type Lease struct {
	Token     uint64
	ExpiresAt time.Time
}

type Manager interface {
	Acquire(ctx context.Context, resource, owner string, ttl time.Duration) (Lease, bool, error)
	Renew(ctx context.Context, resource, owner string, token uint64, ttl time.Duration) (Lease, bool, error)
	Release(ctx context.Context, resource, owner string, token uint64) error
}
