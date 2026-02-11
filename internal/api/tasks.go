package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/VenkatGGG/Browser-use/internal/session"
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

type replayTaskRequest struct {
	SessionID        string `json:"session_id,omitempty"`
	MaxRetries       *int   `json:"max_retries,omitempty"`
	CreateNewSession bool   `json:"create_new_session,omitempty"`
	TenantID         string `json:"tenant_id,omitempty"`
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
	path := strings.TrimPrefix(r.URL.Path, "/v1/tasks/")
	if path == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_task_id", "task id is required")
		return
	}

	parts := strings.Split(path, "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_task_id", "task id is required")
		return
	}
	id := strings.TrimSpace(parts[0])

	if len(parts) == 1 {
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
		return
	}

	if len(parts) == 2 && parts[1] == "replay" {
		if r.Method != http.MethodPost {
			httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		s.replayTask(w, r, id)
		return
	}

	http.NotFound(w, r)
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

	queued, status, ok := s.enqueueCreatedTask(r, created)
	if !ok {
		httpx.WriteJSON(w, status, queued)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, queued)
}

func (s *Server) replayTask(w http.ResponseWriter, r *http.Request, sourceTaskID string) {
	original, err := s.tasks.Get(r.Context(), sourceTaskID)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	var req replayTaskRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
			return
		}
	}

	overrideSessionID := strings.TrimSpace(req.SessionID)
	if req.CreateNewSession && overrideSessionID != "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_replay_request", "session_id cannot be used with create_new_session")
		return
	}

	sessionID := overrideSessionID
	if req.CreateNewSession {
		if s.sessions == nil {
			httpx.WriteError(w, http.StatusInternalServerError, "replay_failed", "session service is not configured")
			return
		}
		tenantID := strings.TrimSpace(req.TenantID)
		if tenantID == "" {
			tenantID = "replay"
		}
		createdSession, err := s.sessions.Create(r.Context(), session.CreateInput{TenantID: tenantID})
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "replay_failed", "failed to create replay session: "+err.Error())
			return
		}
		sessionID = createdSession.ID
	}
	if sessionID == "" {
		sessionID = strings.TrimSpace(original.SessionID)
	}
	maxRetries := original.MaxRetries
	if req.MaxRetries != nil {
		maxRetries = *req.MaxRetries
	}

	created, err := s.tasks.Create(r.Context(), task.CreateInput{
		SourceTaskID: sourceTaskID,
		SessionID:    sessionID,
		URL:          strings.TrimSpace(original.URL),
		Goal:         strings.TrimSpace(original.Goal),
		Actions:      append([]task.Action(nil), original.Actions...),
		MaxRetries:   maxRetries,
	})
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "replay_failed", err.Error())
		return
	}

	queued, status, ok := s.enqueueCreatedTask(r, created)
	if !ok {
		httpx.WriteJSON(w, status, queued)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, queued)
}

func (s *Server) enqueueCreatedTask(r *http.Request, created task.Task) (task.Task, int, bool) {
	if s.dispatcher == nil {
		failed, _ := s.tasks.Fail(r.Context(), task.FailInput{
			TaskID:    created.ID,
			Completed: time.Now().UTC(),
			Error:     "task dispatcher is not configured",
		})
		return failed, http.StatusInternalServerError, false
	}

	if err := s.dispatcher.Enqueue(r.Context(), created.ID); err != nil {
		failed, _ := s.tasks.Fail(r.Context(), task.FailInput{
			TaskID:    created.ID,
			Completed: time.Now().UTC(),
			Error:     err.Error(),
		})
		if errors.Is(err, taskrunner.ErrQueueFull) {
			return failed, http.StatusServiceUnavailable, false
		}
		return failed, http.StatusInternalServerError, false
	}
	return created, http.StatusAccepted, true
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
