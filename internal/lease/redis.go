package lease

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisManager struct {
	client redis.Cmdable
	prefix string
}

func NewRedisManager(client redis.Cmdable, prefix string) *RedisManager {
	normalized := strings.TrimSpace(prefix)
	if normalized == "" {
		normalized = "browseruse:lease"
	}
	return &RedisManager{
		client: client,
		prefix: normalized,
	}
}

func (m *RedisManager) Acquire(ctx context.Context, resource, owner string, ttl time.Duration) (Lease, bool, error) {
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

	seqKey := m.seqKey(resource)
	token, err := m.client.Incr(ctx, seqKey).Uint64()
	if err != nil {
		return Lease{}, false, fmt.Errorf("lease incr token: %w", err)
	}

	leaseKey := m.leaseKey(resource)
	value := fmt.Sprintf("%s|%d", owner, token)
	acquired, err := m.client.SetNX(ctx, leaseKey, value, ttl).Result()
	if err != nil {
		return Lease{}, false, fmt.Errorf("lease setnx: %w", err)
	}
	if !acquired {
		return Lease{}, false, nil
	}

	return Lease{
		Token:     token,
		ExpiresAt: time.Now().UTC().Add(ttl),
	}, true, nil
}

func (m *RedisManager) Release(ctx context.Context, resource, owner string, token uint64) error {
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

	key := m.leaseKey(resource)
	value := fmt.Sprintf("%s|%d", owner, token)
	_, err := releaseLeaseScript.Run(ctx, m.client, []string{key}, value).Int()
	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("lease release: %w", err)
	}
	return nil
}

func (m *RedisManager) Renew(ctx context.Context, resource, owner string, token uint64, ttl time.Duration) (Lease, bool, error) {
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

	key := m.leaseKey(resource)
	value := fmt.Sprintf("%s|%d", owner, token)
	renewed, err := renewLeaseScript.Run(ctx, m.client, []string{key}, value, int64(ttl/time.Millisecond)).Int()
	if err != nil && !errors.Is(err, redis.Nil) {
		return Lease{}, false, fmt.Errorf("lease renew: %w", err)
	}
	if renewed == 0 {
		return Lease{}, false, nil
	}
	return Lease{
		Token:     token,
		ExpiresAt: time.Now().UTC().Add(ttl),
	}, true, nil
}

func (m *RedisManager) leaseKey(resource string) string {
	return m.prefix + ":hold:" + resource
}

func (m *RedisManager) seqKey(resource string) string {
	return m.prefix + ":seq:" + resource
}

var releaseLeaseScript = redis.NewScript(`
local existing = redis.call("GET", KEYS[1])
if not existing then
  return 0
end
if existing == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

var renewLeaseScript = redis.NewScript(`
local existing = redis.call("GET", KEYS[1])
if not existing then
  return 0
end
if existing == ARGV[1] then
  return redis.call("PEXPIRE", KEYS[1], ARGV[2])
end
return 0
`)
