package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/VenkatGGG/Browser-use/internal/pool"
	"github.com/VenkatGGG/Browser-use/internal/session"
	"github.com/VenkatGGG/Browser-use/internal/task"
	"github.com/VenkatGGG/Browser-use/internal/taskrunner"
)

type recordingDispatcher struct {
	lastTaskID string
	err        error
}

func (d *recordingDispatcher) Enqueue(_ context.Context, taskID string) error {
	d.lastTaskID = taskID
	return d.err
}

func TestCreateTaskWithActionsQueued(t *testing.T) {
	dispatcher := &recordingDispatcher{}
	srv := NewServer(
		session.NewInMemoryService(),
		task.NewInMemoryService(),
		pool.NewInMemoryRegistry(),
		dispatcher,
		2,
		"",
		nil,
	)

	body := []byte(`{
		"session_id": "sess_123",
		"url": "https://example.com",
		"goal": "fill form",
		"actions": [
			{"type":"wait_for","selector":"input[name='q']","timeout_ms":3000},
			{"type":"type","selector":"input[name='q']","text":"cats"},
			{"type":"click","selector":"button[type='submit']"}
		]
	}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected task status 202, got %d body=%s", rr.Code, rr.Body.String())
	}

	var created task.Task
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode task response: %v", err)
	}
	if created.Status != task.StatusQueued {
		t.Fatalf("expected task status queued, got %s", created.Status)
	}
	if created.MaxRetries != 2 {
		t.Fatalf("expected default max_retries 2, got %d", created.MaxRetries)
	}
	if len(created.Actions) != 3 {
		t.Fatalf("expected 3 task actions, got %d", len(created.Actions))
	}
	if dispatcher.lastTaskID != created.ID {
		t.Fatalf("expected dispatched task id %s, got %s", created.ID, dispatcher.lastTaskID)
	}
}

func TestCreateTaskQueueFullMarksFailed(t *testing.T) {
	dispatcher := &recordingDispatcher{err: taskrunner.ErrQueueFull}
	svc := task.NewInMemoryService()
	srv := NewServer(
		session.NewInMemoryService(),
		svc,
		pool.NewInMemoryRegistry(),
		dispatcher,
		1,
		"",
		nil,
	)

	body := []byte(`{"session_id":"sess_123","url":"https://example.com","goal":"fill form"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected task status 503, got %d body=%s", rr.Code, rr.Body.String())
	}

	var failed task.Task
	if err := json.Unmarshal(rr.Body.Bytes(), &failed); err != nil {
		t.Fatalf("decode task response: %v", err)
	}
	if failed.Status != task.StatusFailed {
		t.Fatalf("expected task status failed, got %s", failed.Status)
	}
	if !errors.Is(dispatcher.err, taskrunner.ErrQueueFull) {
		t.Fatalf("expected queue full error")
	}

	stored, err := svc.Get(context.Background(), failed.ID)
	if err != nil {
		t.Fatalf("load stored task: %v", err)
	}
	if stored.Status != task.StatusFailed {
		t.Fatalf("expected stored task status failed, got %s", stored.Status)
	}
}

func TestCreateTaskMaxRetriesOverride(t *testing.T) {
	dispatcher := &recordingDispatcher{}
	srv := NewServer(
		session.NewInMemoryService(),
		task.NewInMemoryService(),
		pool.NewInMemoryRegistry(),
		dispatcher,
		1,
		"",
		nil,
	)

	body := []byte(`{"session_id":"sess_123","url":"https://example.com","goal":"fill form","max_retries":5}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected task status 202, got %d body=%s", rr.Code, rr.Body.String())
	}

	var created task.Task
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode task response: %v", err)
	}
	if created.MaxRetries != 5 {
		t.Fatalf("expected max_retries 5, got %d", created.MaxRetries)
	}
}

func TestListRecentTasks(t *testing.T) {
	svc := task.NewInMemoryService()
	srv := NewServer(
		session.NewInMemoryService(),
		svc,
		pool.NewInMemoryRegistry(),
		nil,
		1,
		"",
		nil,
	)

	first, err := svc.Create(context.Background(), task.CreateInput{
		SessionID: "sess_1",
		URL:       "https://example.com/1",
		Goal:      "one",
	})
	if err != nil {
		t.Fatalf("create first task: %v", err)
	}
	time.Sleep(2 * time.Millisecond)

	second, err := svc.Create(context.Background(), task.CreateInput{
		SessionID: "sess_2",
		URL:       "https://example.com/2",
		Goal:      "two",
	})
	if err != nil {
		t.Fatalf("create second task: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/tasks?limit=2", nil)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var payload struct {
		Tasks []task.Task `json:"tasks"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode tasks response: %v", err)
	}

	if len(payload.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(payload.Tasks))
	}
	if payload.Tasks[0].ID != second.ID {
		t.Fatalf("expected first task in list to be %s, got %s", second.ID, payload.Tasks[0].ID)
	}
	if payload.Tasks[1].ID != first.ID {
		t.Fatalf("expected second task in list to be %s, got %s", first.ID, payload.Tasks[1].ID)
	}
}

func TestListRecentTasksInvalidLimit(t *testing.T) {
	srv := NewServer(
		session.NewInMemoryService(),
		task.NewInMemoryService(),
		pool.NewInMemoryRegistry(),
		nil,
		1,
		"",
		nil,
	)

	req := httptest.NewRequest(http.MethodGet, "/v1/tasks?limit=abc", nil)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}
