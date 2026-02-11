package api

import (
	"net/http"

	"github.com/VenkatGGG/Browser-use/internal/pool"
	"github.com/VenkatGGG/Browser-use/internal/session"
	"github.com/VenkatGGG/Browser-use/internal/task"
	"github.com/VenkatGGG/Browser-use/pkg/httpx"
)

type Server struct {
	sessions session.Service
	tasks    task.Service
	nodes    pool.Registry
}

func NewServer(sessions session.Service, tasks task.Service, nodes pool.Registry) *Server {
	return &Server{sessions: sessions, tasks: tasks, nodes: nodes}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/v1/sessions", s.handleSessions)
	mux.HandleFunc("/v1/sessions/", s.handleSessionByID)
	mux.HandleFunc("/v1/tasks", s.handleTasks)
	mux.HandleFunc("/v1/tasks/", s.handleTaskByID)
	mux.HandleFunc("/v1/nodes", s.handleNodes)
	mux.HandleFunc("/v1/nodes/register", s.handleNodeRegister)
	mux.HandleFunc("/v1/nodes/", s.handleNodeByID)

	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
