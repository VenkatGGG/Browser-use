package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/VenkatGGG/Browser-use/internal/task"
	"github.com/VenkatGGG/Browser-use/internal/taskrunner"
	"github.com/VenkatGGG/Browser-use/pkg/httpx"
)

type taskActionRequest struct {
	Type      string `json:"type"`
	Selector  string `json:"selector,omitempty"`
	Text      string `json:"text,omitempty"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
	DelayMS   int    `json:"delay_ms,omitempty"`
}

type createTaskRequest struct {
	SessionID  string              `json:"session_id"`
	URL        string              `json:"url"`
	Goal       string              `json:"goal"`
	Actions    []taskActionRequest `json:"actions,omitempty"`
	MaxRetries *int                `json:"max_retries,omitempty"`
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listRecentTasks(w, r)
	case http.MethodPost:
		s.createAndQueueTask(w, r)
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

func (s *Server) createAndQueueTask(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}

	actions := mapTaskActions(req.Actions)
	maxRetries := s.defaultMaxRetries
	if req.MaxRetries != nil {
		maxRetries = *req.MaxRetries
	}
	created, err := s.tasks.Create(r.Context(), task.CreateInput{
		SessionID:  strings.TrimSpace(req.SessionID),
		URL:        strings.TrimSpace(req.URL),
		Goal:       strings.TrimSpace(req.Goal),
		Actions:    actions,
		MaxRetries: maxRetries,
	})
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "create_failed", err.Error())
		return
	}

	if s.dispatcher == nil {
		failed, _ := s.tasks.Fail(r.Context(), task.FailInput{
			TaskID:    created.ID,
			Completed: time.Now().UTC(),
			Error:     "task dispatcher is not configured",
		})
		httpx.WriteJSON(w, http.StatusInternalServerError, failed)
		return
	}

	if err := s.dispatcher.Enqueue(r.Context(), created.ID); err != nil {
		failed, _ := s.tasks.Fail(r.Context(), task.FailInput{
			TaskID:    created.ID,
			Completed: time.Now().UTC(),
			Error:     err.Error(),
		})
		if errors.Is(err, taskrunner.ErrQueueFull) {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, failed)
			return
		}
		httpx.WriteJSON(w, http.StatusInternalServerError, failed)
		return
	}

	httpx.WriteJSON(w, http.StatusAccepted, created)
}

func (s *Server) listRecentTasks(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_limit", "limit must be a positive integer")
			return
		}
		if parsed > 200 {
			parsed = 200
		}
		limit = parsed
	}

	items, err := s.tasks.ListRecent(r.Context(), limit)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"tasks": items,
	})
}

func mapTaskActions(actions []taskActionRequest) []task.Action {
	mapped := make([]task.Action, 0, len(actions))
	for _, action := range actions {
		mapped = append(mapped, task.Action{
			Type:      strings.TrimSpace(action.Type),
			Selector:  strings.TrimSpace(action.Selector),
			Text:      action.Text,
			TimeoutMS: action.TimeoutMS,
			DelayMS:   action.DelayMS,
		})
	}
	return mapped
}
