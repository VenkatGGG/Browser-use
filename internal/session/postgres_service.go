package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresService struct {
	pool *pgxpool.Pool
}

func NewPostgresService(ctx context.Context, dsn string) (*PostgresService, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("postgres dsn is required")
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}

	svc := &PostgresService{pool: pool}
	if err := svc.initSchema(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return svc, nil
}

func (s *PostgresService) Close() {
	s.pool.Close()
}

func (s *PostgresService) Create(ctx context.Context, input CreateInput) (Session, error) {
	tenantID := strings.TrimSpace(input.TenantID)
	if tenantID == "" {
		return Session{}, errors.New("tenant_id is required")
	}

	id := "sess_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	now := time.Now().UTC()

	row := s.pool.QueryRow(ctx, `
INSERT INTO sessions (id, tenant_id, status, created_at)
VALUES ($1, $2, $3, $4)
RETURNING id, tenant_id, status, created_at
`, id, tenantID, StatusReady, now)
	return scanSession(row)
}

func (s *PostgresService) Delete(ctx context.Context, id string) error {
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return errors.New("session id is required")
	}
	result, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, trimmedID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrSessionNotFound
	}
	return nil
}

func (s *PostgresService) initSchema(ctx context.Context) error {
	statements := []string{
		`
CREATE TABLE IF NOT EXISTS sessions (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	status TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL
);
`,
		`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS tenant_id TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'ready';`,
		`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_created_at ON sessions (created_at DESC);`,
	}

	for _, stmt := range statements {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("initialize sessions schema: %w", err)
		}
	}
	return nil
}

type sessionRowScanner interface {
	Scan(dest ...any) error
}

func scanSession(row sessionRowScanner) (Session, error) {
	var out Session
	var status string
	err := row.Scan(&out.ID, &out.TenantID, &status, &out.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, ErrSessionNotFound
		}
		return Session{}, err
	}
	out.Status = Status(strings.TrimSpace(status))
	out.CreatedAt = out.CreatedAt.UTC()
	return out, nil
}
