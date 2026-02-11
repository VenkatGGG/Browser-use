package main

import (
	"log"
	"net/http"

	"github.com/VenkatGGG/Browser-use/internal/api"
	"github.com/VenkatGGG/Browser-use/internal/config"
	"github.com/VenkatGGG/Browser-use/internal/nodeclient"
	"github.com/VenkatGGG/Browser-use/internal/pool"
	"github.com/VenkatGGG/Browser-use/internal/session"
	"github.com/VenkatGGG/Browser-use/internal/task"
)

func main() {
	cfg := config.Load()
	log.Printf("config loaded: redis=%s postgres=%s", cfg.RedisAddr, cfg.PostgresDSN)

	sessionSvc := session.NewInMemoryService()
	taskSvc := task.NewInMemoryService()
	nodeRegistry := pool.NewInMemoryRegistry()
	executor := nodeclient.NewHTTPClient(cfg.NodeExecuteTimeout)
	server := api.NewServer(sessionSvc, taskSvc, nodeRegistry, executor)

	httpServer := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      server.Routes(),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	log.Printf("orchestrator listening on %s", cfg.HTTPAddr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("orchestrator failed: %v", err)
	}
}
