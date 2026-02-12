package task

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
)

var (
	ErrTaskNotFound  = errors.New("task not found")
	ErrTaskNotQueued = errors.New("task is not queued")
)

type Action struct {
	Type      string `json:"type"`
	Selector  string `json:"selector,omitempty"`
	Text      string `json:"text,omitempty"`
	Pixels    int    `json:"pixels,omitempty"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
	DelayMS   int    `json:"delay_ms,omitempty"`
}

type StepTrace struct {
	Index                 int        `json:"index"`
	Action                Action     `json:"action"`
	Status                string     `json:"status"`
	Error                 string     `json:"error,omitempty"`
	OutputText            string     `json:"output_text,omitempty"`
	StartedAt             *time.Time `json:"started_at,omitempty"`
	CompletedAt           *time.Time `json:"completed_at,omitempty"`
	DurationMS            int64      `json:"duration_ms,omitempty"`
	ScreenshotBase64      string     `json:"screenshot_base64,omitempty"`
	ScreenshotArtifactURL string     `json:"screenshot_artifact_url,omitempty"`
}

type Task struct {
	ID                    string      `json:"id"`
	SourceTaskID          string      `json:"source_task_id,omitempty"`
	SessionID             string      `json:"session_id"`
	URL                   string      `json:"url"`
	Goal                  string      `json:"goal"`
	Actions               []Action    `json:"actions,omitempty"`
	Trace                 []StepTrace `json:"trace,omitempty"`
	ExtractedOutputs      []string    `json:"extracted_outputs,omitempty"`
	Status                Status      `json:"status"`
	Attempt               int         `json:"attempt"`
	MaxRetries            int         `json:"max_retries"`
	NextRetryAt           *time.Time  `json:"next_retry_at,omitempty"`
	NodeID                string      `json:"node_id,omitempty"`
	PageTitle             string      `json:"page_title,omitempty"`
	FinalURL              string      `json:"final_url,omitempty"`
	ScreenshotBase64      string      `json:"screenshot_base64,omitempty"`
	ScreenshotArtifactURL string      `json:"screenshot_artifact_url,omitempty"`
	BlockerType           string      `json:"blocker_type,omitempty"`
	BlockerMessage        string      `json:"blocker_message,omitempty"`
	ErrorMessage          string      `json:"error_message,omitempty"`
	CreatedAt             time.Time   `json:"created_at"`
	StartedAt             *time.Time  `json:"started_at,omitempty"`
	CompletedAt           *time.Time  `json:"completed_at,omitempty"`
}

type CreateInput struct {
	SourceTaskID string
	SessionID    string
	URL          string
	Goal         string
	Actions      []Action
	MaxRetries   int
}

type StartInput struct {
	TaskID  string
	NodeID  string
	Started time.Time
}

type RetryInput struct {
	TaskID    string
	RetryAt   time.Time
	LastError string
}

type CompleteInput struct {
	TaskID                string
	NodeID                string
	Completed             time.Time
	PageTitle             string
	FinalURL              string
	ScreenshotBase64      string
	ScreenshotArtifactURL string
	Trace                 []StepTrace
}

type FailInput struct {
	TaskID                string
	NodeID                string
	Completed             time.Time
	Error                 string
	PageTitle             string
	FinalURL              string
	Screenshot            string
	ScreenshotArtifactURL string
	BlockerType           string
	BlockerMessage        string
	Trace                 []StepTrace
}

type Service interface {
	Create(ctx context.Context, input CreateInput) (Task, error)
	Start(ctx context.Context, input StartInput) (Task, error)
	Retry(ctx context.Context, input RetryInput) (Task, error)
	Complete(ctx context.Context, input CompleteInput) (Task, error)
	Fail(ctx context.Context, input FailInput) (Task, error)
	Get(ctx context.Context, id string) (Task, error)
	ListRecent(ctx context.Context, limit int) ([]Task, error)
	ListBySourceTaskID(ctx context.Context, sourceTaskID string, limit int) ([]Task, error)
	ListQueued(ctx context.Context, limit int) ([]Task, error)
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
	if input.Goal == "" && len(input.Actions) == 0 {
		return Task{}, errors.New("goal is required when actions are empty")
	}
	if input.MaxRetries < 0 {
		return Task{}, errors.New("max_retries cannot be negative")
	}
	id := fmt.Sprintf("task_%06d", s.counter.Add(1))
	now := time.Now().UTC()
	created := Task{
		ID:           id,
		SourceTaskID: input.SourceTaskID,
		SessionID:    input.SessionID,
		URL:          input.URL,
		Goal:         input.Goal,
		Actions:      append([]Action(nil), input.Actions...),
		Status:       StatusQueued,
		MaxRetries:   input.MaxRetries,
		CreatedAt:    now,
	}

	s.mu.Lock()
	s.items[id] = created
	s.mu.Unlock()

	return created, nil
}

func (s *InMemoryService) Start(_ context.Context, input StartInput) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.items[input.TaskID]
	if !ok {
		return Task{}, ErrTaskNotFound
	}
	if task.Status != StatusQueued {
		return Task{}, ErrTaskNotQueued
	}
	now := normalizeTime(input.Started)
	task.Status = StatusRunning
	task.Attempt++
	task.NodeID = input.NodeID
	task.StartedAt = &now
	task.NextRetryAt = nil
	task.ErrorMessage = ""
	task.BlockerType = ""
	task.BlockerMessage = ""
	task.Trace = nil
	task.ExtractedOutputs = nil
	s.items[input.TaskID] = task
	return task, nil
}

func (s *InMemoryService) Retry(_ context.Context, input RetryInput) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.items[input.TaskID]
	if !ok {
		return Task{}, ErrTaskNotFound
	}
	retryAt := normalizeTime(input.RetryAt)
	task.Status = StatusQueued
	task.NodeID = ""
	task.NextRetryAt = &retryAt
	task.ErrorMessage = input.LastError
	task.BlockerType = ""
	task.BlockerMessage = ""
	task.Trace = nil
	task.ExtractedOutputs = nil
	s.items[input.TaskID] = task
	return task, nil
}

func (s *InMemoryService) Complete(_ context.Context, input CompleteInput) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.items[input.TaskID]
	if !ok {
		return Task{}, ErrTaskNotFound
	}
	now := normalizeTime(input.Completed)
	task.Status = StatusCompleted
	task.NodeID = input.NodeID
	task.PageTitle = input.PageTitle
	task.FinalURL = input.FinalURL
	task.ScreenshotBase64 = input.ScreenshotBase64
	task.ScreenshotArtifactURL = input.ScreenshotArtifactURL
	task.BlockerType = ""
	task.BlockerMessage = ""
	task.ErrorMessage = ""
	task.NextRetryAt = nil
	task.Trace = append([]StepTrace(nil), input.Trace...)
	task.ExtractedOutputs = deriveExtractedOutputs(task.Trace)
	task.CompletedAt = &now
	s.items[input.TaskID] = task
	return task, nil
}

func (s *InMemoryService) Fail(_ context.Context, input FailInput) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.items[input.TaskID]
	if !ok {
		return Task{}, ErrTaskNotFound
	}
	now := normalizeTime(input.Completed)
	task.Status = StatusFailed
	task.NodeID = input.NodeID
	task.PageTitle = input.PageTitle
	task.FinalURL = input.FinalURL
	task.ScreenshotBase64 = input.Screenshot
	task.ScreenshotArtifactURL = input.ScreenshotArtifactURL
	task.BlockerType = input.BlockerType
	task.BlockerMessage = input.BlockerMessage
	task.ErrorMessage = input.Error
	task.NextRetryAt = nil
	task.Trace = append([]StepTrace(nil), input.Trace...)
	task.ExtractedOutputs = deriveExtractedOutputs(task.Trace)
	task.CompletedAt = &now
	s.items[input.TaskID] = task
	return task, nil
}

func (s *InMemoryService) Get(_ context.Context, id string) (Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	found, ok := s.items[id]
	if !ok {
		return Task{}, ErrTaskNotFound
	}
	return found, nil
}

func (s *InMemoryService) ListRecent(_ context.Context, limit int) ([]Task, error) {
	if limit <= 0 {
		limit = 50
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]Task, 0, len(s.items))
	for _, item := range s.items {
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})

	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *InMemoryService) ListBySourceTaskID(_ context.Context, sourceTaskID string, limit int) ([]Task, error) {
	if limit <= 0 {
		limit = 50
	}
	sourceTaskID = strings.TrimSpace(sourceTaskID)
	if sourceTaskID == "" {
		return []Task{}, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]Task, 0, len(s.items))
	for _, item := range s.items {
		if item.SourceTaskID != sourceTaskID {
			continue
		}
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *InMemoryService) ListQueued(_ context.Context, limit int) ([]Task, error) {
	if limit <= 0 {
		limit = 100
	}

	now := time.Now().UTC()

	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]Task, 0, len(s.items))
	for _, item := range s.items {
		if item.Status != StatusQueued {
			continue
		}
		if item.NextRetryAt != nil && item.NextRetryAt.After(now) {
			continue
		}
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		if left.NextRetryAt != nil && right.NextRetryAt != nil {
			return left.NextRetryAt.Before(*right.NextRetryAt)
		}
		if left.NextRetryAt != nil {
			return true
		}
		if right.NextRetryAt != nil {
			return false
		}
		return left.CreatedAt.Before(right.CreatedAt)
	})

	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func normalizeTime(input time.Time) time.Time {
	if input.IsZero() {
		return time.Now().UTC()
	}
	return input.UTC()
}

func deriveExtractedOutputs(trace []StepTrace) []string {
	if len(trace) == 0 {
		return nil
	}

	outputs := make([]string, 0, len(trace))
	seen := make(map[string]struct{}, len(trace))
	for _, step := range trace {
		value := strings.TrimSpace(step.OutputText)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		outputs = append(outputs, value)
		if len(outputs) >= 20 {
			break
		}
	}
	if len(outputs) == 0 {
		return nil
	}
	return outputs
}
