package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/VenkatGGG/Browser-use/internal/pool"
	"github.com/VenkatGGG/Browser-use/internal/session"
	"github.com/VenkatGGG/Browser-use/internal/task"
	"github.com/VenkatGGG/Browser-use/internal/taskrunner"
)

type recordingDispatcher struct {
	lastTaskID string
	taskIDs    []string
	err        error
}

func (d *recordingDispatcher) Enqueue(_ context.Context, taskID string) error {
	d.lastTaskID = taskID
	d.taskIDs = append(d.taskIDs, taskID)
	return d.err
}

type immediateCompleteDispatcher struct {
	svc task.Service
}

func (d *immediateCompleteDispatcher) Enqueue(_ context.Context, taskID string) error {
	go func() {
		time.Sleep(25 * time.Millisecond)
		_, _ = d.svc.Start(context.Background(), task.StartInput{
			TaskID:  taskID,
			NodeID:  "node-1",
			Started: time.Now().UTC(),
		})
		_, _ = d.svc.Complete(context.Background(), task.CompleteInput{
			TaskID:    taskID,
			NodeID:    "node-1",
			Completed: time.Now().UTC(),
			PageTitle: "done",
			FinalURL:  "https://example.com/done",
		})
	}()
	return nil
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

func TestCreateTaskLegacyAliasQueued(t *testing.T) {
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

	body := []byte(`{"session_id":"sess_legacy","url":"https://example.com","goal":"open","wait_for_completion":false}`)
	req := httptest.NewRequest(http.MethodPost, "/task", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", rr.Code, rr.Body.String())
	}

	var created task.Task
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode task response: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("expected task id")
	}
	if dispatcher.lastTaskID != created.ID {
		t.Fatalf("expected dispatched task id %s, got %s", created.ID, dispatcher.lastTaskID)
	}
}

func TestCreateTaskAutoCreatesSessionWhenMissing(t *testing.T) {
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

	body := []byte(`{"url":"https://example.com","goal":"open","wait_for_completion":false}`)
	req := httptest.NewRequest(http.MethodPost, "/task", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", rr.Code, rr.Body.String())
	}

	var created task.Task
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode task response: %v", err)
	}
	if !strings.HasPrefix(created.SessionID, "sess_") {
		t.Fatalf("expected auto-created session id, got %q", created.SessionID)
	}
}

func TestTaskAliasDefaultsToSynchronousWait(t *testing.T) {
	svc := task.NewInMemoryService()
	dispatcher := &immediateCompleteDispatcher{svc: svc}
	srv := NewServer(
		session.NewInMemoryService(),
		svc,
		pool.NewInMemoryRegistry(),
		dispatcher,
		1,
		"",
		nil,
	)

	body := []byte(`{"url":"https://example.com","goal":"open"}`)
	req := httptest.NewRequest(http.MethodPost, "/task", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for /task default synchronous mode, got %d body=%s", rr.Code, rr.Body.String())
	}

	var found task.Task
	if err := json.Unmarshal(rr.Body.Bytes(), &found); err != nil {
		t.Fatalf("decode task response: %v", err)
	}
	if found.Status != task.StatusCompleted {
		t.Fatalf("expected completed task in sync mode, got %s", found.Status)
	}
}

func TestV1TasksDefaultsToAsyncMode(t *testing.T) {
	svc := task.NewInMemoryService()
	dispatcher := &immediateCompleteDispatcher{svc: svc}
	srv := NewServer(
		session.NewInMemoryService(),
		svc,
		pool.NewInMemoryRegistry(),
		dispatcher,
		1,
		"",
		nil,
	)

	body := []byte(`{"session_id":"sess_1","url":"https://example.com","goal":"open"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202 for /v1/tasks default async mode, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestCreateTaskMissingSessionFailsWhenSessionServiceUnavailable(t *testing.T) {
	dispatcher := &recordingDispatcher{}
	srv := NewServer(
		nil,
		task.NewInMemoryService(),
		pool.NewInMemoryRegistry(),
		dispatcher,
		1,
		"",
		nil,
	)

	body := []byte(`{"url":"https://example.com","goal":"open"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when session service unavailable, got %d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "session_id is required") == false {
		t.Fatalf("expected session_id error, got body=%s", rr.Body.String())
	}
}

func TestCreateTaskIdempotencyKeyReturnsSameTaskAndSingleDispatch(t *testing.T) {
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

	body1 := []byte(`{"session_id":"sess_1","url":"https://example.com","goal":"open"}`)
	req1 := httptest.NewRequest(http.MethodPost, "/v1/tasks", bytes.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Idempotency-Key", "task-key-1")
	rr1 := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusAccepted {
		t.Fatalf("expected first task status 202, got %d body=%s", rr1.Code, rr1.Body.String())
	}

	body2 := []byte(`{"session_id":"sess_1","url":"https://example.com","goal":"open changed"}`)
	req2 := httptest.NewRequest(http.MethodPost, "/v1/tasks", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Idempotency-Key", "task-key-1")
	rr2 := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusAccepted {
		t.Fatalf("expected second task status 202, got %d body=%s", rr2.Code, rr2.Body.String())
	}

	var first task.Task
	var second task.Task
	if err := json.Unmarshal(rr1.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode first task: %v", err)
	}
	if err := json.Unmarshal(rr2.Body.Bytes(), &second); err != nil {
		t.Fatalf("decode second task: %v", err)
	}
	if first.ID == "" || second.ID == "" {
		t.Fatalf("expected non-empty task ids")
	}
	if first.ID != second.ID {
		t.Fatalf("expected same task id for idempotent requests, got %s and %s", first.ID, second.ID)
	}
	if len(dispatcher.taskIDs) != 1 {
		t.Fatalf("expected single dispatch, got %d", len(dispatcher.taskIDs))
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

func TestCreateTaskWaitForCompletionReturnsTerminalTask(t *testing.T) {
	svc := task.NewInMemoryService()
	dispatcher := &immediateCompleteDispatcher{svc: svc}
	srv := NewServer(
		session.NewInMemoryService(),
		svc,
		pool.NewInMemoryRegistry(),
		dispatcher,
		1,
		"",
		nil,
	)

	body := []byte(`{
		"session_id":"sess_wait",
		"url":"https://example.com",
		"goal":"open and complete",
		"wait_for_completion":true,
		"wait_timeout_ms":3000
	}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected task status 200 when waited to terminal, got %d body=%s", rr.Code, rr.Body.String())
	}

	var found task.Task
	if err := json.Unmarshal(rr.Body.Bytes(), &found); err != nil {
		t.Fatalf("decode task response: %v", err)
	}
	if found.Status != task.StatusCompleted {
		t.Fatalf("expected completed task, got %s", found.Status)
	}
	if found.PageTitle != "done" {
		t.Fatalf("expected completed metadata, got title=%q", found.PageTitle)
	}
}

func TestCreateTaskWaitForCompletionTimeoutReturnsAccepted(t *testing.T) {
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

	body := []byte(`{
		"session_id":"sess_wait",
		"url":"https://example.com",
		"goal":"open and complete",
		"wait_for_completion":true,
		"wait_timeout_ms":1000
	}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected task status 202 on wait timeout, got %d body=%s", rr.Code, rr.Body.String())
	}

	var found task.Task
	if err := json.Unmarshal(rr.Body.Bytes(), &found); err != nil {
		t.Fatalf("decode task response: %v", err)
	}
	if found.Status != task.StatusQueued {
		t.Fatalf("expected queued task on timeout, got %s", found.Status)
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

func TestGetTaskLegacyAliasByID(t *testing.T) {
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

	created, err := svc.Create(context.Background(), task.CreateInput{
		SessionID: "sess_1",
		URL:       "https://example.com",
		Goal:      "open",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/tasks/"+created.ID, nil)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var found task.Task
	if err := json.Unmarshal(rr.Body.Bytes(), &found); err != nil {
		t.Fatalf("decode task response: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("expected id %s, got %s", created.ID, found.ID)
	}
}

func TestTaskStats(t *testing.T) {
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
	if _, err := svc.Complete(context.Background(), task.CompleteInput{
		TaskID:    first.ID,
		Completed: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("complete first task: %v", err)
	}

	second, err := svc.Create(context.Background(), task.CreateInput{
		SessionID: "sess_2",
		URL:       "https://example.com/2",
		Goal:      "two",
	})
	if err != nil {
		t.Fatalf("create second task: %v", err)
	}
	if _, err := svc.Fail(context.Background(), task.FailInput{
		TaskID:         second.ID,
		Completed:      time.Now().UTC(),
		Error:          "blocked",
		BlockerType:    "human_verification_required",
		BlockerMessage: "challenge",
	}); err != nil {
		t.Fatalf("fail second task: %v", err)
	}

	third, err := svc.Create(context.Background(), task.CreateInput{
		SessionID: "sess_3",
		URL:       "https://example.com/3",
		Goal:      "three",
	})
	if err != nil {
		t.Fatalf("create third task: %v", err)
	}
	if _, err := svc.Fail(context.Background(), task.FailInput{
		TaskID:    third.ID,
		Completed: time.Now().UTC(),
		Error:     "other error",
	}); err != nil {
		t.Fatalf("fail third task: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/tasks/stats?limit=10", nil)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var payload struct {
		StatusCounts       map[string]int `json:"status_counts"`
		SuccessRatePercent int            `json:"success_rate_percent"`
		BlockRatePercent   int            `json:"block_rate_percent"`
		Totals             struct {
			Tasks   int `json:"tasks"`
			Blocked int `json:"blocked"`
		} `json:"totals"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode stats response: %v", err)
	}
	if payload.Totals.Tasks != 3 {
		t.Fatalf("expected totals.tasks 3, got %d", payload.Totals.Tasks)
	}
	if payload.Totals.Blocked != 1 {
		t.Fatalf("expected totals.blocked 1, got %d", payload.Totals.Blocked)
	}
	if payload.StatusCounts[string(task.StatusCompleted)] != 1 {
		t.Fatalf("expected completed count 1, got %d", payload.StatusCounts[string(task.StatusCompleted)])
	}
	if payload.StatusCounts[string(task.StatusFailed)] != 2 {
		t.Fatalf("expected failed count 2, got %d", payload.StatusCounts[string(task.StatusFailed)])
	}
	if payload.SuccessRatePercent != 33 {
		t.Fatalf("expected success rate 33, got %d", payload.SuccessRatePercent)
	}
	if payload.BlockRatePercent != 50 {
		t.Fatalf("expected block rate 50, got %d", payload.BlockRatePercent)
	}
}

func TestTaskStatsInvalidLimit(t *testing.T) {
	srv := NewServer(
		session.NewInMemoryService(),
		task.NewInMemoryService(),
		pool.NewInMemoryRegistry(),
		nil,
		1,
		"",
		nil,
	)

	req := httptest.NewRequest(http.MethodGet, "/v1/tasks/stats?limit=abc", nil)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestReplayTaskQueued(t *testing.T) {
	dispatcher := &recordingDispatcher{}
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

	original, err := svc.Create(context.Background(), task.CreateInput{
		SessionID:  "sess_original",
		URL:        "https://example.com",
		Goal:       "open page",
		Actions:    []task.Action{{Type: "wait", DelayMS: 300}},
		MaxRetries: 2,
	})
	if err != nil {
		t.Fatalf("create original task: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+original.ID+"/replay", nil)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", rr.Code, rr.Body.String())
	}

	var replayed task.Task
	if err := json.Unmarshal(rr.Body.Bytes(), &replayed); err != nil {
		t.Fatalf("decode replay response: %v", err)
	}
	if replayed.ID == original.ID {
		t.Fatalf("expected new task id, got same %s", replayed.ID)
	}
	if replayed.SourceTaskID != original.ID {
		t.Fatalf("expected source_task_id %s, got %s", original.ID, replayed.SourceTaskID)
	}
	if replayed.SessionID != original.SessionID {
		t.Fatalf("expected session_id %s, got %s", original.SessionID, replayed.SessionID)
	}
	if replayed.URL != original.URL {
		t.Fatalf("expected url %s, got %s", original.URL, replayed.URL)
	}
	if replayed.Goal != original.Goal {
		t.Fatalf("expected goal %s, got %s", original.Goal, replayed.Goal)
	}
	if replayed.MaxRetries != original.MaxRetries {
		t.Fatalf("expected max_retries %d, got %d", original.MaxRetries, replayed.MaxRetries)
	}
	if len(replayed.Actions) != len(original.Actions) {
		t.Fatalf("expected %d actions, got %d", len(original.Actions), len(replayed.Actions))
	}
	if dispatcher.lastTaskID != replayed.ID {
		t.Fatalf("expected dispatcher task id %s, got %s", replayed.ID, dispatcher.lastTaskID)
	}
}

func TestReplayTaskWithOverrides(t *testing.T) {
	dispatcher := &recordingDispatcher{}
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

	original, err := svc.Create(context.Background(), task.CreateInput{
		SessionID:  "sess_original",
		URL:        "https://example.com",
		Goal:       "open page",
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("create original task: %v", err)
	}

	body := []byte(`{"session_id":"sess_override","max_retries":4}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+original.ID+"/replay", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", rr.Code, rr.Body.String())
	}

	var replayed task.Task
	if err := json.Unmarshal(rr.Body.Bytes(), &replayed); err != nil {
		t.Fatalf("decode replay response: %v", err)
	}
	if replayed.SourceTaskID != original.ID {
		t.Fatalf("expected source_task_id %s, got %s", original.ID, replayed.SourceTaskID)
	}
	if replayed.SessionID != "sess_override" {
		t.Fatalf("expected overridden session id, got %s", replayed.SessionID)
	}
	if replayed.MaxRetries != 4 {
		t.Fatalf("expected overridden max_retries 4, got %d", replayed.MaxRetries)
	}
}

func TestReplayTaskWithFreshSession(t *testing.T) {
	dispatcher := &recordingDispatcher{}
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

	original, err := svc.Create(context.Background(), task.CreateInput{
		SessionID:  "sess_original",
		URL:        "https://example.com",
		Goal:       "open page",
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("create original task: %v", err)
	}

	body := []byte(`{"create_new_session":true,"tenant_id":"replay-tenant","max_retries":3}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+original.ID+"/replay", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", rr.Code, rr.Body.String())
	}

	var replayed task.Task
	if err := json.Unmarshal(rr.Body.Bytes(), &replayed); err != nil {
		t.Fatalf("decode replay response: %v", err)
	}
	if replayed.SourceTaskID != original.ID {
		t.Fatalf("expected source_task_id %s, got %s", original.ID, replayed.SourceTaskID)
	}
	if replayed.SessionID == original.SessionID {
		t.Fatalf("expected replayed task to use new session id")
	}
	if replayed.MaxRetries != 3 {
		t.Fatalf("expected max_retries 3, got %d", replayed.MaxRetries)
	}
}

func TestReplayTaskRejectsConflictingSessionInputs(t *testing.T) {
	dispatcher := &recordingDispatcher{}
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

	original, err := svc.Create(context.Background(), task.CreateInput{
		SessionID:  "sess_original",
		URL:        "https://example.com",
		Goal:       "open page",
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("create original task: %v", err)
	}

	body := []byte(`{"session_id":"sess_override","create_new_session":true}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+original.ID+"/replay", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestReplayTaskIdempotencyKeyReturnsSameTaskAndSingleDispatch(t *testing.T) {
	dispatcher := &recordingDispatcher{}
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

	original, err := svc.Create(context.Background(), task.CreateInput{
		SessionID:  "sess_original",
		URL:        "https://example.com",
		Goal:       "open page",
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("create original task: %v", err)
	}

	body1 := []byte(`{"max_retries":2}`)
	req1 := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+original.ID+"/replay", bytes.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Idempotency-Key", "replay-key-1")
	rr1 := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusAccepted {
		t.Fatalf("expected first replay status 202, got %d body=%s", rr1.Code, rr1.Body.String())
	}

	body2 := []byte(`{"max_retries":5}`)
	req2 := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+original.ID+"/replay", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Idempotency-Key", "replay-key-1")
	rr2 := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusAccepted {
		t.Fatalf("expected second replay status 202, got %d body=%s", rr2.Code, rr2.Body.String())
	}

	var first task.Task
	var second task.Task
	if err := json.Unmarshal(rr1.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode first replay task: %v", err)
	}
	if err := json.Unmarshal(rr2.Body.Bytes(), &second); err != nil {
		t.Fatalf("decode second replay task: %v", err)
	}
	if first.ID == "" || second.ID == "" {
		t.Fatalf("expected non-empty replay task ids")
	}
	if first.ID != second.ID {
		t.Fatalf("expected same replay task id for idempotent requests, got %s and %s", first.ID, second.ID)
	}

	dispatchCount := 0
	for _, taskID := range dispatcher.taskIDs {
		if taskID == first.ID {
			dispatchCount++
		}
	}
	if dispatchCount != 1 {
		t.Fatalf("expected replay task to be dispatched once, got %d", dispatchCount)
	}
}

func TestReplayChain(t *testing.T) {
	dispatcher := &recordingDispatcher{}
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

	root, err := svc.Create(context.Background(), task.CreateInput{
		SessionID:  "sess_root",
		URL:        "https://example.com",
		Goal:       "root",
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("create root task: %v", err)
	}

	replayReq := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+root.ID+"/replay", nil)
	replayRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(replayRR, replayReq)
	if replayRR.Code != http.StatusAccepted {
		t.Fatalf("replay expected 202, got %d body=%s", replayRR.Code, replayRR.Body.String())
	}
	var child task.Task
	if err := json.Unmarshal(replayRR.Body.Bytes(), &child); err != nil {
		t.Fatalf("decode replay child: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/tasks/"+child.ID+"/replay_chain?max_depth=5", nil)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var payload struct {
		Tasks     []task.Task `json:"tasks"`
		Truncated bool        `json:"truncated"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode replay chain response: %v", err)
	}
	if payload.Truncated {
		t.Fatalf("expected non-truncated chain")
	}
	if len(payload.Tasks) != 2 {
		t.Fatalf("expected 2 tasks in chain, got %d", len(payload.Tasks))
	}
	if payload.Tasks[0].ID != child.ID {
		t.Fatalf("expected first chain task %s, got %s", child.ID, payload.Tasks[0].ID)
	}
	if payload.Tasks[1].ID != root.ID {
		t.Fatalf("expected second chain task %s, got %s", root.ID, payload.Tasks[1].ID)
	}
}

func TestReplayChainInvalidDepth(t *testing.T) {
	srv := NewServer(
		session.NewInMemoryService(),
		task.NewInMemoryService(),
		pool.NewInMemoryRegistry(),
		nil,
		1,
		"",
		nil,
	)

	req := httptest.NewRequest(http.MethodGet, "/v1/tasks/task_1/replay_chain?max_depth=zero", nil)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestListDirectReplays(t *testing.T) {
	dispatcher := &recordingDispatcher{}
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

	root, err := svc.Create(context.Background(), task.CreateInput{
		SessionID:  "sess_root",
		URL:        "https://example.com",
		Goal:       "root",
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("create root task: %v", err)
	}

	for i := 0; i < 2; i++ {
		replayReq := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+root.ID+"/replay", nil)
		replayRR := httptest.NewRecorder()
		srv.Routes().ServeHTTP(replayRR, replayReq)
		if replayRR.Code != http.StatusAccepted {
			t.Fatalf("replay expected 202, got %d body=%s", replayRR.Code, replayRR.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/tasks/"+root.ID+"/replays?limit=50", nil)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var payload struct {
		SourceTaskID string      `json:"source_task_id"`
		Tasks        []task.Task `json:"tasks"`
		Count        int         `json:"count"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode direct replays response: %v", err)
	}
	if payload.SourceTaskID != root.ID {
		t.Fatalf("expected source_task_id %s, got %s", root.ID, payload.SourceTaskID)
	}
	if len(payload.Tasks) != 2 {
		t.Fatalf("expected 2 direct replay tasks, got %d", len(payload.Tasks))
	}
	for _, item := range payload.Tasks {
		if item.SourceTaskID != root.ID {
			t.Fatalf("expected child source_task_id %s, got %s", root.ID, item.SourceTaskID)
		}
	}
	if payload.Count != 2 {
		t.Fatalf("expected count 2, got %d", payload.Count)
	}
}
