package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/VenkatGGG/Browser-use/internal/task"
	"github.com/VenkatGGG/Browser-use/pkg/httpx"
)

type createTaskRequest struct {
	SessionID string `json:"session_id"`
	URL       string `json:"url"`
	Goal      string `json:"goal"`
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req createTaskRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
			return
		}
		created, err := s.tasks.Create(r.Context(), task.CreateInput{
			SessionID: strings.TrimSpace(req.SessionID),
			URL:       strings.TrimSpace(req.URL),
			Goal:      strings.TrimSpace(req.Goal),
		})
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "create_failed", err.Error())
			return
		}
		httpx.WriteJSON(w, http.StatusAccepted, created)
	default:
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *Server) handleTaskByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/v1/tasks/")
	if id == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_task_id", "task id is required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		found, err := s.tasks.Get(r.Context(), id)
		if err != nil {
			httpx.WriteError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		httpx.WriteJSON(w, http.StatusOK, found)
	default:
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}
