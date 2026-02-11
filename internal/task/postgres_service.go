package task

import (
	"context"
	"encoding/json"
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

func (s *PostgresService) Create(ctx context.Context, input CreateInput) (Task, error) {
	if input.SessionID == "" {
		return Task{}, errors.New("session_id is required")
	}
	if input.URL == "" {
		return Task{}, errors.New("url is required")
	}
	if input.Goal == "" && len(input.Actions) == 0 {
		return Task{}, errors.New("goal is required when actions are empty")
	}
	if input.MaxRetries < 0 {
		return Task{}, errors.New("max_retries cannot be negative")
	}

	actionsJSON, err := json.Marshal(input.Actions)
	if err != nil {
		return Task{}, fmt.Errorf("marshal actions: %w", err)
	}

	taskID := "task_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	now := time.Now().UTC()

	row := s.pool.QueryRow(ctx, `
INSERT INTO tasks (
	id, session_id, url, goal, actions, status, attempt, max_retries,
	next_retry_at, node_id, page_title, final_url, screenshot_base64,
	screenshot_artifact_url, error_message, created_at, started_at, completed_at
) VALUES (
	$1, $2, $3, $4, $5::jsonb, $6, 0, $7,
	NULL, NULL, NULL, NULL, NULL,
	NULL, NULL, $8, NULL, NULL
)
RETURNING `+taskColumns, taskID, input.SessionID, input.URL, input.Goal, actionsJSON, StatusQueued, input.MaxRetries, now)

	return scanTask(row)
}

func (s *PostgresService) Start(ctx context.Context, input StartInput) (Task, error) {
	now := normalizeTime(input.Started)

	row := s.pool.QueryRow(ctx, `
UPDATE tasks
SET
	status = $2,
	attempt = attempt + 1,
	node_id = $3,
	started_at = $4,
	next_retry_at = NULL,
	error_message = '',
	completed_at = NULL
WHERE id = $1 AND status = $5
RETURNING `+taskColumns,
		input.TaskID,
		StatusRunning,
		nullableString(input.NodeID),
		now,
		StatusQueued,
	)

	updated, err := scanTask(row)
	if err == nil {
		return updated, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		if _, stateErr := s.fetchTaskStatus(ctx, input.TaskID); stateErr != nil {
			return Task{}, stateErr
		}
		return Task{}, ErrTaskNotQueued
	}
	return Task{}, err
}

func (s *PostgresService) Retry(ctx context.Context, input RetryInput) (Task, error) {
	retryAt := normalizeTime(input.RetryAt)
	row := s.pool.QueryRow(ctx, `
UPDATE tasks
SET
	status = $2,
	node_id = NULL,
	next_retry_at = $3,
	error_message = $4
WHERE id = $1
RETURNING `+taskColumns,
		input.TaskID,
		StatusQueued,
		retryAt,
		input.LastError,
	)

	updated, err := scanTask(row)
	if err == nil {
		return updated, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, ErrTaskNotFound
	}
	return Task{}, err
}

func (s *PostgresService) Complete(ctx context.Context, input CompleteInput) (Task, error) {
	now := normalizeTime(input.Completed)
	row := s.pool.QueryRow(ctx, `
UPDATE tasks
SET
	status = $2,
	node_id = $3,
	page_title = $4,
	final_url = $5,
	screenshot_base64 = $6,
	screenshot_artifact_url = $7,
	error_message = '',
	next_retry_at = NULL,
	completed_at = $8
WHERE id = $1
RETURNING `+taskColumns,
		input.TaskID,
		StatusCompleted,
		nullableString(input.NodeID),
		nullableString(input.PageTitle),
		nullableString(input.FinalURL),
		nullableString(input.ScreenshotBase64),
		nullableString(input.ScreenshotArtifactURL),
		now,
	)

	updated, err := scanTask(row)
	if err == nil {
		return updated, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, ErrTaskNotFound
	}
	return Task{}, err
}

func (s *PostgresService) Fail(ctx context.Context, input FailInput) (Task, error) {
	now := normalizeTime(input.Completed)
	row := s.pool.QueryRow(ctx, `
UPDATE tasks
SET
	status = $2,
	node_id = $3,
	page_title = $4,
	final_url = $5,
	screenshot_base64 = $6,
	error_message = $7,
	next_retry_at = NULL,
	completed_at = $8
WHERE id = $1
RETURNING `+taskColumns,
		input.TaskID,
		StatusFailed,
		nullableString(input.NodeID),
		nullableString(input.PageTitle),
		nullableString(input.FinalURL),
		nullableString(input.Screenshot),
		input.Error,
		now,
	)

	updated, err := scanTask(row)
	if err == nil {
		return updated, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, ErrTaskNotFound
	}
	return Task{}, err
}

func (s *PostgresService) Get(ctx context.Context, id string) (Task, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+taskColumns+` FROM tasks WHERE id = $1`, id)
	found, err := scanTask(row)
	if err == nil {
		return found, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, ErrTaskNotFound
	}
	return Task{}, err
}

func (s *PostgresService) ListQueued(ctx context.Context, limit int) ([]Task, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.pool.Query(ctx, `
SELECT `+taskColumns+`
FROM tasks
WHERE
	status = $1
	AND (next_retry_at IS NULL OR next_retry_at <= NOW())
ORDER BY COALESCE(next_retry_at, created_at) ASC, created_at ASC
LIMIT $2
`, StatusQueued, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Task, 0, limit)
	for rows.Next() {
		item, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *PostgresService) initSchema(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS tasks (
	id TEXT PRIMARY KEY,
	session_id TEXT NOT NULL,
	url TEXT NOT NULL,
	goal TEXT NOT NULL,
	actions JSONB NOT NULL DEFAULT '[]'::jsonb,
	status TEXT NOT NULL,
	attempt INTEGER NOT NULL DEFAULT 0,
	max_retries INTEGER NOT NULL DEFAULT 0,
	next_retry_at TIMESTAMPTZ NULL,
	node_id TEXT NULL,
	page_title TEXT NULL,
	final_url TEXT NULL,
	screenshot_base64 TEXT NULL,
	screenshot_artifact_url TEXT NULL,
	error_message TEXT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	started_at TIMESTAMPTZ NULL,
	completed_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_tasks_status_retry
ON tasks (status, next_retry_at);
`)
	if err != nil {
		return fmt.Errorf("initialize tasks schema: %w", err)
	}
	return nil
}

func (s *PostgresService) fetchTaskStatus(ctx context.Context, taskID string) (Status, error) {
	var status Status
	err := s.pool.QueryRow(ctx, `SELECT status FROM tasks WHERE id = $1`, taskID).Scan(&status)
	if err == nil {
		return status, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrTaskNotFound
	}
	return "", err
}

type rowScanner interface {
	Scan(dest ...any) error
}

const taskColumns = `
id,
session_id,
url,
goal,
actions,
status,
attempt,
max_retries,
next_retry_at,
node_id,
page_title,
final_url,
screenshot_base64,
screenshot_artifact_url,
error_message,
created_at,
started_at,
completed_at`

func scanTask(row rowScanner) (Task, error) {
	var item Task
	var actionsJSON []byte
	var nextRetryAt *time.Time
	var nodeID *string
	var pageTitle *string
	var finalURL *string
	var screenshotBase64 *string
	var screenshotArtifactURL *string
	var errorMessage *string
	var startedAt *time.Time
	var completedAt *time.Time

	err := row.Scan(
		&item.ID,
		&item.SessionID,
		&item.URL,
		&item.Goal,
		&actionsJSON,
		&item.Status,
		&item.Attempt,
		&item.MaxRetries,
		&nextRetryAt,
		&nodeID,
		&pageTitle,
		&finalURL,
		&screenshotBase64,
		&screenshotArtifactURL,
		&errorMessage,
		&item.CreatedAt,
		&startedAt,
		&completedAt,
	)
	if err != nil {
		return Task{}, err
	}

	if len(actionsJSON) > 0 {
		if err := json.Unmarshal(actionsJSON, &item.Actions); err != nil {
			return Task{}, fmt.Errorf("decode task actions: %w", err)
		}
	}

	item.CreatedAt = item.CreatedAt.UTC()
	item.NextRetryAt = utcPtr(nextRetryAt)
	item.StartedAt = utcPtr(startedAt)
	item.CompletedAt = utcPtr(completedAt)
	item.NodeID = deref(nodeID)
	item.PageTitle = deref(pageTitle)
	item.FinalURL = deref(finalURL)
	item.ScreenshotBase64 = deref(screenshotBase64)
	item.ScreenshotArtifactURL = deref(screenshotArtifactURL)
	item.ErrorMessage = deref(errorMessage)

	return item, nil
}

func nullableString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func utcPtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	v := value.UTC()
	return &v
}
