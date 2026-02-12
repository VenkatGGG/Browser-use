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
