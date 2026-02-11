package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/VenkatGGG/Browser-use/internal/nodeclient"
	"github.com/VenkatGGG/Browser-use/internal/pool"
	"github.com/VenkatGGG/Browser-use/internal/task"
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
	SessionID string              `json:"session_id"`
	URL       string              `json:"url"`
	Goal      string              `json:"goal"`
	Actions   []taskActionRequest `json:"actions,omitempty"`
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.createAndExecuteTask(w, r)
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

func (s *Server) createAndExecuteTask(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}

	actions := mapTaskActions(req.Actions)
	created, err := s.tasks.Create(r.Context(), task.CreateInput{
		SessionID: strings.TrimSpace(req.SessionID),
		URL:       strings.TrimSpace(req.URL),
		Goal:      strings.TrimSpace(req.Goal),
		Actions:   actions,
	})
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "create_failed", err.Error())
		return
	}

	node, err := s.pickReadyNode(r)
	if err != nil {
		failed, _ := s.tasks.Fail(r.Context(), task.FailInput{
			TaskID:    created.ID,
			Completed: time.Now().UTC(),
			Error:     err.Error(),
		})
		httpx.WriteJSON(w, http.StatusServiceUnavailable, failed)
		return
	}

	if _, err := s.tasks.Start(r.Context(), task.StartInput{
		TaskID:  created.ID,
		NodeID:  node.ID,
		Started: time.Now().UTC(),
	}); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "task_start_failed", err.Error())
		return
	}

	result, err := s.executor.Execute(r.Context(), node.Address, nodeclient.ExecuteInput{
		TaskID:  created.ID,
		URL:     created.URL,
		Goal:    created.Goal,
		Actions: mapNodeActions(created.Actions),
	})
	if err != nil {
		failed, failErr := s.tasks.Fail(r.Context(), task.FailInput{
			TaskID:    created.ID,
			NodeID:    node.ID,
			Completed: time.Now().UTC(),
			Error:     err.Error(),
		})
		if failErr != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "task_failed", failErr.Error())
			return
		}
		httpx.WriteJSON(w, http.StatusBadGateway, failed)
		return
	}

	completed, err := s.tasks.Complete(r.Context(), task.CompleteInput{
		TaskID:           created.ID,
		NodeID:           node.ID,
		Completed:        time.Now().UTC(),
		PageTitle:        result.PageTitle,
		FinalURL:         result.FinalURL,
		ScreenshotBase64: result.ScreenshotBase64,
	})
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "task_complete_failed", err.Error())
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, completed)
}

func (s *Server) pickReadyNode(r *http.Request) (pool.Node, error) {
	nodes, err := s.nodes.List(r.Context())
	if err != nil {
		return pool.Node{}, err
	}
	for _, node := range nodes {
		if node.State == pool.NodeStateReady {
			return node, nil
		}
	}
	return pool.Node{}, errors.New("no ready nodes available")
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

func mapNodeActions(actions []task.Action) []nodeclient.Action {
	mapped := make([]nodeclient.Action, 0, len(actions))
	for _, action := range actions {
		mapped = append(mapped, nodeclient.Action{
			Type:      action.Type,
			Selector:  action.Selector,
			Text:      action.Text,
			TimeoutMS: action.TimeoutMS,
			DelayMS:   action.DelayMS,
		})
	}
	return mapped
}
