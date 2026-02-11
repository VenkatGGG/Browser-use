package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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
