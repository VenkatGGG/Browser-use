package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/VenkatGGG/Browser-use/internal/api"
	"github.com/VenkatGGG/Browser-use/internal/artifact"
	"github.com/VenkatGGG/Browser-use/internal/config"
	"github.com/VenkatGGG/Browser-use/internal/nodeclient"
	"github.com/VenkatGGG/Browser-use/internal/pool"
	"github.com/VenkatGGG/Browser-use/internal/session"
	"github.com/VenkatGGG/Browser-use/internal/task"
	"github.com/VenkatGGG/Browser-use/internal/taskrunner"
)

func main() {
	cfg := config.Load()
	log.Printf(
		"config loaded: redis=%s postgres=%s queue_size=%d workers=%d max_retries=%d artifacts_dir=%s",
		cfg.RedisAddr,
		cfg.PostgresDSN,
		cfg.TaskQueueSize,
		cfg.TaskWorkers,
		cfg.TaskDefaultMaxRetries,
		cfg.ArtifactDir,
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	sessionSvc := session.NewInMemoryService()
	taskSvc := task.NewInMemoryService()
	nodeRegistry := pool.NewInMemoryRegistry()
	executor := nodeclient.NewHTTPClient(cfg.NodeExecuteTimeout)

	artifactStore, err := artifact.NewLocalStore(cfg.ArtifactDir, cfg.ArtifactBaseURL)
	if err != nil {
		log.Fatalf("initialize artifact store: %v", err)
	}
	artifactHandler := http.StripPrefix(cfg.ArtifactBaseURL+"/", http.FileServer(http.Dir(cfg.ArtifactDir)))

	runner := taskrunner.New(taskSvc, nodeRegistry, executor, artifactStore, taskrunner.Config{
		QueueSize:       cfg.TaskQueueSize,
		Workers:         cfg.TaskWorkers,
		NodeWaitTimeout: cfg.NodeWaitTimeout,
		RetryBaseDelay:  cfg.TaskRetryBaseDelay,
		RetryMaxDelay:   cfg.TaskRetryMaxDelay,
	}, log.Default())
	runner.Start(ctx)

	server := api.NewServer(
		sessionSvc,
		taskSvc,
		nodeRegistry,
		runner,
		cfg.TaskDefaultMaxRetries,
		cfg.ArtifactBaseURL,
		artifactHandler,
	)

	httpServer := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      server.Routes(),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	go func() {
		log.Printf("orchestrator listening on %s", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("orchestrator failed: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("orchestrator shutdown error: %v", err)
	}
}
