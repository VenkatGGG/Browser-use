package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/VenkatGGG/Browser-use/internal/nodeclient"
	"github.com/VenkatGGG/Browser-use/internal/pool"
	"github.com/VenkatGGG/Browser-use/internal/session"
	"github.com/VenkatGGG/Browser-use/internal/task"
)

type recordingExecutor struct {
	lastInput nodeclient.ExecuteInput
}

func (r *recordingExecutor) Execute(_ context.Context, _ string, input nodeclient.ExecuteInput) (nodeclient.ExecuteOutput, error) {
	r.lastInput = input
	return nodeclient.ExecuteOutput{
		PageTitle:        "test",
		FinalURL:         input.URL,
		ScreenshotBase64: "abc",
	}, nil
}

func TestCreateTaskWithActions(t *testing.T) {
	executor := &recordingExecutor{}
	srv := NewServer(
		session.NewInMemoryService(),
		task.NewInMemoryService(),
		pool.NewInMemoryRegistry(),
		executor,
	)

	registerReq := httptest.NewRequest(http.MethodPost, "/v1/nodes/register", bytes.NewReader([]byte(`{"node_id":"node-1","address":"node:8091","version":"dev"}`)))
	registerReq.Header.Set("Content-Type", "application/json")
	registerRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(registerRR, registerReq)
	if registerRR.Code != http.StatusCreated {
		t.Fatalf("expected register status 201, got %d", registerRR.Code)
	}

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

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected task status 201, got %d body=%s", rr.Code, rr.Body.String())
	}

	var created task.Task
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode task response: %v", err)
	}
	if created.Status != task.StatusCompleted {
		t.Fatalf("expected task status completed, got %s", created.Status)
	}
	if len(created.Actions) != 3 {
		t.Fatalf("expected 3 task actions, got %d", len(created.Actions))
	}
	if len(executor.lastInput.Actions) != 3 {
		t.Fatalf("expected 3 execute actions, got %d", len(executor.lastInput.Actions))
	}
	if executor.lastInput.Actions[0].Type != "wait_for" {
		t.Fatalf("expected first action wait_for, got %s", executor.lastInput.Actions[0].Type)
	}
}
