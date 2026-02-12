package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/VenkatGGG/Browser-use/internal/pool"
	"github.com/VenkatGGG/Browser-use/internal/session"
	"github.com/VenkatGGG/Browser-use/internal/task"
)

type fakeNodeRecycler struct {
	destroyed []string
	err       error
}

func (f *fakeNodeRecycler) DestroyNode(_ context.Context, nodeID string) error {
	if f.err != nil {
		return f.err
	}
	f.destroyed = append(f.destroyed, nodeID)
	return nil
}

func TestHealthz(t *testing.T) {
	srv := NewServer(
		session.NewInMemoryService(),
		task.NewInMemoryService(),
		pool.NewInMemoryRegistry(),
		nil,
		1,
		"",
		nil,
	)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
}

func TestNodeRegisterAndList(t *testing.T) {
	srv := NewServer(
		session.NewInMemoryService(),
		task.NewInMemoryService(),
		pool.NewInMemoryRegistry(),
		nil,
		1,
		"",
		nil,
	)

	registerBody := map[string]string{
		"node_id":   "node-1",
		"address":   "browser-node:8091",
		"version":   "dev",
		"booted_at": "2026-02-11T09:00:00Z",
	}
	rawRegisterBody, err := json.Marshal(registerBody)
	if err != nil {
		t.Fatalf("marshal register body: %v", err)
	}

	registerReq := httptest.NewRequest(http.MethodPost, "/v1/nodes/register", bytes.NewReader(rawRegisterBody))
	registerRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(registerRR, registerReq)
	if registerRR.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", registerRR.Code)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/v1/nodes", nil)
	listRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", listRR.Code)
	}
}

func TestNodeDrainActivateAndRecycle(t *testing.T) {
	registry := pool.NewInMemoryRegistry()
	srv := NewServer(
		session.NewInMemoryService(),
		task.NewInMemoryService(),
		registry,
		nil,
		1,
		"",
		nil,
	)
	recycler := &fakeNodeRecycler{}
	srv.SetNodeRecycler(recycler)

	registerBody := map[string]string{
		"node_id":   "poolnode-1",
		"address":   "poolnode-1:9091",
		"version":   "dev",
		"booted_at": "2026-02-11T09:00:00Z",
	}
	rawRegisterBody, err := json.Marshal(registerBody)
	if err != nil {
		t.Fatalf("marshal register body: %v", err)
	}

	registerReq := httptest.NewRequest(http.MethodPost, "/v1/nodes/register", bytes.NewReader(rawRegisterBody))
	registerRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(registerRR, registerReq)
	if registerRR.Code != http.StatusCreated {
		t.Fatalf("expected register status 201, got %d body=%s", registerRR.Code, registerRR.Body.String())
	}

	drainReq := httptest.NewRequest(http.MethodPost, "/v1/nodes/poolnode-1/drain", nil)
	drainRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(drainRR, drainReq)
	if drainRR.Code != http.StatusOK {
		t.Fatalf("expected drain status 200, got %d body=%s", drainRR.Code, drainRR.Body.String())
	}
	var drained pool.Node
	if err := json.Unmarshal(drainRR.Body.Bytes(), &drained); err != nil {
		t.Fatalf("decode drain response: %v", err)
	}
	if drained.State != pool.NodeStateDraining {
		t.Fatalf("expected draining state, got %s", drained.State)
	}

	activateReq := httptest.NewRequest(http.MethodPost, "/v1/nodes/poolnode-1/activate", nil)
	activateRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(activateRR, activateReq)
	if activateRR.Code != http.StatusOK {
		t.Fatalf("expected activate status 200, got %d body=%s", activateRR.Code, activateRR.Body.String())
	}
	var activated pool.Node
	if err := json.Unmarshal(activateRR.Body.Bytes(), &activated); err != nil {
		t.Fatalf("decode activate response: %v", err)
	}
	if activated.State != pool.NodeStateReady {
		t.Fatalf("expected ready state, got %s", activated.State)
	}

	recycleReq := httptest.NewRequest(http.MethodPost, "/v1/nodes/poolnode-1/recycle", nil)
	recycleRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(recycleRR, recycleReq)
	if recycleRR.Code != http.StatusOK {
		t.Fatalf("expected recycle status 200, got %d body=%s", recycleRR.Code, recycleRR.Body.String())
	}
	var recycled pool.Node
	if err := json.Unmarshal(recycleRR.Body.Bytes(), &recycled); err != nil {
		t.Fatalf("decode recycle response: %v", err)
	}
	if recycled.State != pool.NodeStateDead {
		t.Fatalf("expected dead state after recycle, got %s", recycled.State)
	}
	if len(recycler.destroyed) != 1 || recycler.destroyed[0] != "poolnode-1" {
		t.Fatalf("expected recycled node to be destroyed, got %#v", recycler.destroyed)
	}
}

func TestDashboardRoute(t *testing.T) {
	srv := NewServer(
		session.NewInMemoryService(),
		task.NewInMemoryService(),
		pool.NewInMemoryRegistry(),
		nil,
		1,
		"",
		nil,
	)

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	body, err := io.ReadAll(rr.Body)
	if err != nil {
		t.Fatalf("read dashboard body: %v", err)
	}
	if !strings.Contains(string(body), "Browser Use Control Room") {
		t.Fatalf("dashboard response missing expected title")
	}
}

func TestMetricsRoute(t *testing.T) {
	registry := pool.NewInMemoryRegistry()
	taskSvc := task.NewInMemoryService()
	srv := NewServer(
		session.NewInMemoryService(),
		taskSvc,
		registry,
		nil,
		1,
		"",
		nil,
	)

	_, _ = registry.Register(t.Context(), pool.RegisterInput{
		NodeID:  "node-1",
		Address: "node-1:9091",
		Version: "dev",
	})

	completedTask, err := taskSvc.Create(t.Context(), task.CreateInput{
		SessionID: "sess_a",
		URL:       "https://example.com",
		Goal:      "ok",
	})
	if err != nil {
		t.Fatalf("create completed task: %v", err)
	}
	_, err = taskSvc.Complete(t.Context(), task.CompleteInput{
		TaskID:    completedTask.ID,
		NodeID:    "node-1",
		Completed: completedTask.CreatedAt.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatalf("complete task: %v", err)
	}

	failedTask, err := taskSvc.Create(t.Context(), task.CreateInput{
		SessionID: "sess_b",
		URL:       "https://example.com/fail",
		Goal:      "fail",
	})
	if err != nil {
		t.Fatalf("create failed task: %v", err)
	}
	_, err = taskSvc.Fail(t.Context(), task.FailInput{
		TaskID:         failedTask.ID,
		Completed:      failedTask.CreatedAt.Add(3 * time.Second),
		Error:          "blocked",
		BlockerType:    "human_verification_required",
		BlockerMessage: "captcha",
	})
	if err != nil {
		t.Fatalf("fail task: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics?limit=20", nil)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected metrics status 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("expected text/plain metrics content type, got %q", rr.Header().Get("Content-Type"))
	}
	body := rr.Body.String()
	if !strings.Contains(body, "browseruse_tasks_status_total{status=\"completed\"} 1") {
		t.Fatalf("metrics body missing completed status count: %s", body)
	}
	if !strings.Contains(body, "browseruse_tasks_status_total{status=\"failed\"} 1") {
		t.Fatalf("metrics body missing failed status count: %s", body)
	}
	if !strings.Contains(body, "browseruse_nodes_state_total{state=\"ready\"} 1") {
		t.Fatalf("metrics body missing node ready count: %s", body)
	}
	if !strings.Contains(body, "browseruse_tasks_blocker_total{blocker_type=\"human_verification_required\"} 1") {
		t.Fatalf("metrics body missing blocker count: %s", body)
	}
}

func TestAPISecurityRequiresAPIKeyOnCreateRoutes(t *testing.T) {
	srv := NewServer(
		session.NewInMemoryService(),
		task.NewInMemoryService(),
		pool.NewInMemoryRegistry(),
		nil,
		1,
		"",
		nil,
	)
	srv.SetAPISecurity("topsecret", 0)

	unauthorizedReq := httptest.NewRequest(http.MethodPost, "/v1/sessions", bytes.NewReader([]byte(`{"tenant_id":"secure"}`)))
	unauthorizedReq.Header.Set("Content-Type", "application/json")
	unauthorizedRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(unauthorizedRR, unauthorizedReq)
	if unauthorizedRR.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without API key, got %d body=%s", unauthorizedRR.Code, unauthorizedRR.Body.String())
	}

	authorizedReq := httptest.NewRequest(http.MethodPost, "/v1/sessions", bytes.NewReader([]byte(`{"tenant_id":"secure"}`)))
	authorizedReq.Header.Set("Content-Type", "application/json")
	authorizedReq.Header.Set("X-API-Key", "topsecret")
	authorizedRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(authorizedRR, authorizedReq)
	if authorizedRR.Code != http.StatusCreated {
		t.Fatalf("expected 201 with API key, got %d body=%s", authorizedRR.Code, authorizedRR.Body.String())
	}
}

func TestAPISecurityRateLimitOnCreateRoutes(t *testing.T) {
	srv := NewServer(
		session.NewInMemoryService(),
		task.NewInMemoryService(),
		pool.NewInMemoryRegistry(),
		nil,
		1,
		"",
		nil,
	)
	srv.SetAPISecurity("", 2)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/sessions", bytes.NewReader([]byte(`{"tenant_id":"rate"}`)))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "10.1.2.3:12345"
		rr := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("expected 201 for request %d, got %d body=%s", i+1, rr.Code, rr.Body.String())
		}
	}

	limitedReq := httptest.NewRequest(http.MethodPost, "/v1/sessions", bytes.NewReader([]byte(`{"tenant_id":"rate"}`)))
	limitedReq.Header.Set("Content-Type", "application/json")
	limitedReq.RemoteAddr = "10.1.2.3:12345"
	limitedRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(limitedRR, limitedReq)
	if limitedRR.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after rate limit exceeded, got %d body=%s", limitedRR.Code, limitedRR.Body.String())
	}

	// Read-only endpoints remain accessible even when create routes are throttled.
	healthReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(healthRR, healthReq)
	if healthRR.Code != http.StatusOK {
		t.Fatalf("expected healthz status 200, got %d", healthRR.Code)
	}
}

func TestLegacySessionRoutes(t *testing.T) {
	srv := NewServer(
		session.NewInMemoryService(),
		task.NewInMemoryService(),
		pool.NewInMemoryRegistry(),
		nil,
		1,
		"",
		nil,
	)

	createBody := []byte(`{"tenant_id":"legacy"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("expected create status 201, got %d body=%s", createRR.Code, createRR.Body.String())
	}

	var created session.Session
	if err := json.Unmarshal(createRR.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created session: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("expected session id")
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/sessions/"+created.ID, nil)
	deleteRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(deleteRR, deleteReq)
	if deleteRR.Code != http.StatusNoContent {
		t.Fatalf("expected delete status 204, got %d body=%s", deleteRR.Code, deleteRR.Body.String())
	}
}

func TestCreateSessionIdempotencyKeyReturnsSameSession(t *testing.T) {
	srv := NewServer(
		session.NewInMemoryService(),
		task.NewInMemoryService(),
		pool.NewInMemoryRegistry(),
		nil,
		1,
		"",
		nil,
	)

	body1 := []byte(`{"tenant_id":"idem"}`)
	req1 := httptest.NewRequest(http.MethodPost, "/v1/sessions", bytes.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Idempotency-Key", "sess-key-1")
	rr1 := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusCreated {
		t.Fatalf("expected first create status 201, got %d body=%s", rr1.Code, rr1.Body.String())
	}

	body2 := []byte(`{"tenant_id":"idem-changed"}`)
	req2 := httptest.NewRequest(http.MethodPost, "/v1/sessions", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Idempotency-Key", "sess-key-1")
	rr2 := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusCreated {
		t.Fatalf("expected second create status 201, got %d body=%s", rr2.Code, rr2.Body.String())
	}

	var first session.Session
	var second session.Session
	if err := json.Unmarshal(rr1.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode first session: %v", err)
	}
	if err := json.Unmarshal(rr2.Body.Bytes(), &second); err != nil {
		t.Fatalf("decode second session: %v", err)
	}
	if first.ID == "" || second.ID == "" {
		t.Fatalf("expected non-empty session ids")
	}
	if first.ID != second.ID {
		t.Fatalf("expected idempotent response with same id, got %s and %s", first.ID, second.ID)
	}
}
