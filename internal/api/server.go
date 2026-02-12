package api

import (
	"context"
	"net/http"
	"time"

	"github.com/VenkatGGG/Browser-use/internal/idempotency"
	"github.com/VenkatGGG/Browser-use/internal/pool"
	"github.com/VenkatGGG/Browser-use/internal/session"
	"github.com/VenkatGGG/Browser-use/internal/task"
	"github.com/VenkatGGG/Browser-use/pkg/httpx"
)

type TaskDispatcher interface {
	Enqueue(ctx context.Context, taskID string) error
}

type Server struct {
	sessions          session.Service
	tasks             task.Service
	nodes             pool.Registry
	dispatcher        TaskDispatcher
	defaultMaxRetries int
	artifactPath      string
	artifactHandler   http.Handler
	idempotency       idempotency.Store
	idempotencyTTL    time.Duration
	idempotencyLock   time.Duration
}

func NewServer(sessions session.Service, tasks task.Service, nodes pool.Registry, dispatcher TaskDispatcher, defaultMaxRetries int, artifactPath string, artifactHandler http.Handler) *Server {
	if defaultMaxRetries < 0 {
		defaultMaxRetries = 0
	}
	return &Server{
		sessions:          sessions,
		tasks:             tasks,
		nodes:             nodes,
		dispatcher:        dispatcher,
		defaultMaxRetries: defaultMaxRetries,
		artifactPath:      artifactPath,
		artifactHandler:   artifactHandler,
		idempotency:       idempotency.NewInMemoryStore(),
		idempotencyTTL:    24 * time.Hour,
		idempotencyLock:   30 * time.Second,
	}
}

func (s *Server) SetIdempotencyStore(store idempotency.Store, ttl, lockTTL time.Duration) {
	if store != nil {
		s.idempotency = store
	}
	if ttl > 0 {
		s.idempotencyTTL = ttl
	}
	if lockTTL > 0 {
		s.idempotencyLock = lockTTL
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/", s.handleDashboard)
	mux.HandleFunc("/dashboard", s.handleDashboard)
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/sessions", s.handleSessions)
	mux.HandleFunc("/sessions/", s.handleSessionByID)
	mux.HandleFunc("/v1/sessions", s.handleSessions)
	mux.HandleFunc("/v1/sessions/", s.handleSessionByID)
	mux.HandleFunc("/task", s.handleTaskAlias)
	mux.HandleFunc("/tasks/", s.handleTaskAliasByID)
	mux.HandleFunc("/v1/tasks", s.handleTasks)
	mux.HandleFunc("/v1/tasks/stats", s.handleTaskStats)
	mux.HandleFunc("/v1/tasks/", s.handleTaskByID)
	mux.HandleFunc("/v1/nodes", s.handleNodes)
	mux.HandleFunc("/v1/nodes/register", s.handleNodeRegister)
	mux.HandleFunc("/v1/nodes/", s.handleNodeByID)
	if s.artifactHandler != nil {
		path := s.artifactPath
		if path == "" {
			path = "/artifacts"
		}
		if path[len(path)-1] != '/' {
			path += "/"
		}
		mux.Handle(path, s.artifactHandler)
	}

	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
