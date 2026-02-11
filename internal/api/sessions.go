package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/VenkatGGG/Browser-use/internal/session"
	"github.com/VenkatGGG/Browser-use/pkg/httpx"
)

type createSessionRequest struct {
	TenantID string `json:"tenant_id"`
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req createSessionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
			return
		}
		created, err := s.sessions.Create(r.Context(), session.CreateInput{TenantID: strings.TrimSpace(req.TenantID)})
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "create_failed", err.Error())
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, created)
	default:
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *Server) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/v1/sessions/"))
	if id == r.URL.Path {
		id = strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/sessions/"))
	}
	if id == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_session_id", "session id is required")
		return
	}

	switch r.Method {
	case http.MethodDelete:
		if err := s.sessions.Delete(r.Context(), id); err != nil {
			httpx.WriteError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}
