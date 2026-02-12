package idempotency

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisStore struct {
	client redis.Cmdable
	prefix string
}

func NewRedisStore(client redis.Cmdable, prefix string) *RedisStore {
	normalized := strings.TrimSpace(prefix)
	if normalized == "" {
		normalized = "browseruse:idempotency"
	}
	return &RedisStore{
		client: client,
		prefix: normalized,
	}
}

func (s *RedisStore) Get(ctx context.Context, scope, key string) (Entry, bool, error) {
	compound, err := normalizeCompoundKey(scope, key)
	if err != nil {
		return Entry{}, false, err
	}
	raw, err := s.client.Get(ctx, s.responseKey(compound)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return Entry{}, false, nil
		}
		return Entry{}, false, fmt.Errorf("idempotency get: %w", err)
	}
	var entry Entry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return Entry{}, false, fmt.Errorf("decode idempotency entry: %w", err)
	}
	return entry, true, nil
}

func (s *RedisStore) Claim(ctx context.Context, scope, key, owner string, ttl time.Duration) (bool, error) {
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
	ok, err := s.client.SetNX(ctx, s.lockKey(compound), owner, ttl).Result()
	if err != nil {
		return false, fmt.Errorf("idempotency claim: %w", err)
	}
	return ok, nil
}

func (s *RedisStore) Save(ctx context.Context, scope, key string, entry Entry, ttl time.Duration) error {
	compound, err := normalizeCompoundKey(scope, key)
	if err != nil {
		return err
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode idempotency entry: %w", err)
	}
	if err := s.client.Set(ctx, s.responseKey(compound), raw, ttl).Err(); err != nil {
		return fmt.Errorf("idempotency save: %w", err)
	}
	return nil
}

func (s *RedisStore) Release(ctx context.Context, scope, key, owner string) error {
	compound, err := normalizeCompoundKey(scope, key)
	if err != nil {
		return err
	}
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return errors.New("owner is required")
	}
	_, err = releaseLockScript.Run(ctx, s.client, []string{s.lockKey(compound)}, owner).Int()
	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("idempotency release: %w", err)
	}
	return nil
}

func (s *RedisStore) responseKey(compound string) string {
	return s.prefix + ":resp:" + compound
}

func (s *RedisStore) lockKey(compound string) string {
	return s.prefix + ":lock:" + compound
}

var releaseLockScript = redis.NewScript(`
local existing = redis.call("GET", KEYS[1])
if not existing then
  return 0
end
if existing == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)
