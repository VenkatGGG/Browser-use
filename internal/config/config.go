package config

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/VenkatGGG/Browser-use/internal/artifact"
)

type Config struct {
	HTTPAddr                string
	ReadTimeout             time.Duration
	WriteTimeout            time.Duration
	IdleTimeout             time.Duration
	NodeExecuteTimeout      time.Duration
	TaskQueueSize           int
	TaskWorkers             int
	NodeWaitTimeout         time.Duration
	TaskDefaultMaxRetries   int
	TaskRetryBaseDelay      time.Duration
	TaskRetryMaxDelay       time.Duration
	TaskDomainBlockCooldown time.Duration
	ArtifactDir             string
	ArtifactBaseURL         string
	RedisAddr               string
	PostgresDSN             string
}

func Load() Config {
	return Config{
		HTTPAddr:                envOrDefault("ORCHESTRATOR_HTTP_ADDR", ":8080"),
		ReadTimeout:             durationOrDefault("ORCHESTRATOR_READ_TIMEOUT", 15*time.Second),
		WriteTimeout:            durationOrDefault("ORCHESTRATOR_WRITE_TIMEOUT", 15*time.Second),
		IdleTimeout:             durationOrDefault("ORCHESTRATOR_IDLE_TIMEOUT", 60*time.Second),
		NodeExecuteTimeout:      durationOrDefault("ORCHESTRATOR_NODE_EXEC_TIMEOUT", 45*time.Second),
		TaskQueueSize:           intOrDefault("ORCHESTRATOR_TASK_QUEUE_SIZE", 256),
		TaskWorkers:             intOrDefault("ORCHESTRATOR_TASK_WORKERS", 1),
		NodeWaitTimeout:         durationOrDefault("ORCHESTRATOR_NODE_WAIT_TIMEOUT", 30*time.Second),
		TaskDefaultMaxRetries:   intOrDefault("ORCHESTRATOR_TASK_MAX_RETRIES", 2),
		TaskRetryBaseDelay:      durationOrDefault("ORCHESTRATOR_TASK_RETRY_BASE_DELAY", 1*time.Second),
		TaskRetryMaxDelay:       durationOrDefault("ORCHESTRATOR_TASK_RETRY_MAX_DELAY", 20*time.Second),
		TaskDomainBlockCooldown: durationOrDefault("ORCHESTRATOR_TASK_DOMAIN_BLOCK_COOLDOWN", 3*time.Minute),
		ArtifactDir:             artifact.RootDirFromEnv(os.Getenv("ORCHESTRATOR_ARTIFACTS_DIR")),
		ArtifactBaseURL:         normalizeArtifactBaseURL(os.Getenv("ORCHESTRATOR_ARTIFACT_BASE_URL")),
		RedisAddr:               envOrDefault("REDIS_ADDR", "redis:6379"),
		PostgresDSN:             envOrDefault("POSTGRES_DSN", "postgres://browseruse:browseruse@postgres:5432/browseruse?sslmode=disable"),
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func durationOrDefault(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func intOrDefault(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func normalizeArtifactBaseURL(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "/artifacts"
	}
	if !strings.HasPrefix(trimmed, "/") {
		trimmed = "/" + trimmed
	}
	normalized := strings.TrimSuffix(trimmed, "/")
	if normalized == "" {
		return "/artifacts"
	}
	return normalized
}
