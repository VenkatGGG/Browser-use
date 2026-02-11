package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/VenkatGGG/Browser-use/internal/pool"
	"github.com/VenkatGGG/Browser-use/pkg/httpx"
)

type registerNodeRequest struct {
	NodeID  string `json:"node_id"`
	Address string `json:"address"`
	Version string `json:"version"`
	Booted  string `json:"booted_at"`
}

type heartbeatNodeRequest struct {
	State     string `json:"state"`
	Heartbeat string `json:"heartbeat_at"`
}

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		nodes, err := s.nodes.List(r.Context())
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "list_failed", err.Error())
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"nodes": nodes})
	default:
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *Server) handleNodeRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	var req registerNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}

	bootedAt, err := parseRFC3339(req.Booted)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_booted_at", "booted_at must be RFC3339")
		return
	}

	node, err := s.nodes.Register(r.Context(), pool.RegisterInput{
		NodeID:  strings.TrimSpace(req.NodeID),
		Address: strings.TrimSpace(req.Address),
		Version: strings.TrimSpace(req.Version),
		Booted:  bootedAt,
	})
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "register_failed", err.Error())
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, node)
}

func (s *Server) handleNodeByID(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/v1/nodes/")
	parts := strings.Split(trimmed, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_node_path", "expected /v1/nodes/{id}/heartbeat")
		return
	}

	nodeID := strings.TrimSpace(parts[0])
	action := strings.TrimSpace(parts[1])
	if action != "heartbeat" {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "route not found")
		return
	}

	if r.Method != http.MethodPost {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	var req heartbeatNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}

	heartbeatAt, err := parseRFC3339(req.Heartbeat)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_heartbeat_at", "heartbeat_at must be RFC3339")
		return
	}
	state, err := pool.ParseNodeState(req.State)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_state", err.Error())
		return
	}

	node, err := s.nodes.Heartbeat(r.Context(), pool.HeartbeatInput{
		NodeID: nodeID,
		State:  state,
		At:     heartbeatAt,
	})
	if err != nil {
		if errors.Is(err, pool.ErrNodeNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		httpx.WriteError(w, http.StatusBadRequest, "heartbeat_failed", err.Error())
		return
	}

	httpx.WriteJSON(w, http.StatusOK, node)
}

func parseRFC3339(value string) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, trimmed)
}
