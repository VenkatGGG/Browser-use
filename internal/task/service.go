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
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
)

type Action struct {
	Type      string `json:"type"`
	Selector  string `json:"selector,omitempty"`
	Text      string `json:"text,omitempty"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
	DelayMS   int    `json:"delay_ms,omitempty"`
}

type Task struct {
	ID               string     `json:"id"`
	SessionID        string     `json:"session_id"`
	URL              string     `json:"url"`
	Goal             string     `json:"goal"`
	Actions          []Action   `json:"actions,omitempty"`
	Status           Status     `json:"status"`
	NodeID           string     `json:"node_id,omitempty"`
	PageTitle        string     `json:"page_title,omitempty"`
	FinalURL         string     `json:"final_url,omitempty"`
	ScreenshotBase64 string     `json:"screenshot_base64,omitempty"`
	ErrorMessage     string     `json:"error_message,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
}

type CreateInput struct {
	SessionID string
	URL       string
	Goal      string
	Actions   []Action
}

type StartInput struct {
	TaskID  string
	NodeID  string
	Started time.Time
}

type CompleteInput struct {
	TaskID           string
	NodeID           string
	Completed        time.Time
	PageTitle        string
	FinalURL         string
	ScreenshotBase64 string
}

type FailInput struct {
	TaskID     string
	NodeID     string
	Completed  time.Time
	Error      string
	PageTitle  string
	FinalURL   string
	Screenshot string
}

type Service interface {
	Create(ctx context.Context, input CreateInput) (Task, error)
	Start(ctx context.Context, input StartInput) (Task, error)
	Complete(ctx context.Context, input CompleteInput) (Task, error)
	Fail(ctx context.Context, input FailInput) (Task, error)
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
	if input.Goal == "" && len(input.Actions) == 0 {
		return Task{}, errors.New("goal is required when actions are empty")
	}
	id := fmt.Sprintf("task_%06d", s.counter.Add(1))
	now := time.Now().UTC()
	created := Task{
		ID:        id,
		SessionID: input.SessionID,
		URL:       input.URL,
		Goal:      input.Goal,
		Actions:   append([]Action(nil), input.Actions...),
		Status:    StatusQueued,
		CreatedAt: now,
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
		return Task{}, errors.New("task not found")
	}
	now := normalizeTime(input.Started)
	task.Status = StatusRunning
	task.NodeID = input.NodeID
	task.StartedAt = &now
	task.ErrorMessage = ""
	s.items[input.TaskID] = task
	return task, nil
}

func (s *InMemoryService) Complete(_ context.Context, input CompleteInput) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.items[input.TaskID]
	if !ok {
		return Task{}, errors.New("task not found")
	}
	now := normalizeTime(input.Completed)
	task.Status = StatusCompleted
	task.NodeID = input.NodeID
	task.PageTitle = input.PageTitle
	task.FinalURL = input.FinalURL
	task.ScreenshotBase64 = input.ScreenshotBase64
	task.ErrorMessage = ""
	task.CompletedAt = &now
	s.items[input.TaskID] = task
	return task, nil
}

func (s *InMemoryService) Fail(_ context.Context, input FailInput) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.items[input.TaskID]
	if !ok {
		return Task{}, errors.New("task not found")
	}
	now := normalizeTime(input.Completed)
	task.Status = StatusFailed
	task.NodeID = input.NodeID
	task.PageTitle = input.PageTitle
	task.FinalURL = input.FinalURL
	task.ScreenshotBase64 = input.Screenshot
	task.ErrorMessage = input.Error
	task.CompletedAt = &now
	s.items[input.TaskID] = task
	return task, nil
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

func normalizeTime(input time.Time) time.Time {
	if input.IsZero() {
		return time.Now().UTC()
	}
	return input.UTC()
}
