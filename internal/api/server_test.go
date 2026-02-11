package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/VenkatGGG/Browser-use/internal/session"
	"github.com/VenkatGGG/Browser-use/internal/task"
)

func TestHealthz(t *testing.T) {
	srv := NewServer(session.NewInMemoryService(), task.NewInMemoryService())
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
}
