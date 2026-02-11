package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/VenkatGGG/Browser-use/internal/nodeclient"
	"github.com/VenkatGGG/Browser-use/internal/pool"
	"github.com/VenkatGGG/Browser-use/internal/session"
	"github.com/VenkatGGG/Browser-use/internal/task"
)

func TestHealthz(t *testing.T) {
	srv := NewServer(
		session.NewInMemoryService(),
		task.NewInMemoryService(),
		pool.NewInMemoryRegistry(),
		nodeclient.NoopClient{},
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
		nodeclient.NoopClient{},
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
