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
	NodeLeaseTTL            time.Duration
	IdempotencyTTL          time.Duration
	IdempotencyLockTTL      time.Duration
	ArtifactDir             string
	ArtifactBaseURL         string
	RedisAddr               string
	PostgresDSN             string
	PoolEnabled             bool
	PoolTargetReady         int
	PoolReconcileInterval   time.Duration
	PoolHeartbeatTimeout    time.Duration
	PoolNodeMaxAge          time.Duration
	PoolManagedNodePrefix   string
	PoolDockerImage         string
	PoolDockerNetwork       string
	PoolOrchestratorURL     string
	PoolNodeVersion         string
	PoolNodeHeartbeat       time.Duration
	PoolNodeRequestTimeout  time.Duration
	PoolNodeCDPBaseURL      string
	PoolNodeRenderDelay     time.Duration
	PoolNodeExecuteTimeout  time.Duration
	PoolNodePlannerMode     string
	PoolXVFBScreenGeometry  string
	PoolChromeDebugPort     int
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
		NodeLeaseTTL:            durationOrDefault("ORCHESTRATOR_NODE_LEASE_TTL", 90*time.Second),
		IdempotencyTTL:          durationOrDefault("ORCHESTRATOR_IDEMPOTENCY_TTL", 24*time.Hour),
		IdempotencyLockTTL:      durationOrDefault("ORCHESTRATOR_IDEMPOTENCY_LOCK_TTL", 30*time.Second),
		ArtifactDir:             artifact.RootDirFromEnv(os.Getenv("ORCHESTRATOR_ARTIFACTS_DIR")),
		ArtifactBaseURL:         normalizeArtifactBaseURL(os.Getenv("ORCHESTRATOR_ARTIFACT_BASE_URL")),
		RedisAddr:               envOrDefault("REDIS_ADDR", "redis:6379"),
		PostgresDSN:             envOrDefault("POSTGRES_DSN", "postgres://browseruse:browseruse@postgres:5432/browseruse?sslmode=disable"),
		PoolEnabled:             boolOrDefault("ORCHESTRATOR_POOL_ENABLED", false),
		PoolTargetReady:         intOrDefault("ORCHESTRATOR_POOL_TARGET_READY", 0),
		PoolReconcileInterval:   durationOrDefault("ORCHESTRATOR_POOL_RECONCILE_INTERVAL", 5*time.Second),
		PoolHeartbeatTimeout:    durationOrDefault("ORCHESTRATOR_POOL_HEARTBEAT_TIMEOUT", 30*time.Second),
		PoolNodeMaxAge:          durationOrDefault("ORCHESTRATOR_POOL_NODE_MAX_AGE", 1*time.Hour),
		PoolManagedNodePrefix:   envOrDefault("ORCHESTRATOR_POOL_NODE_ID_PREFIX", "poolnode-"),
		PoolDockerImage:         envOrDefault("ORCHESTRATOR_POOL_DOCKER_IMAGE", "browseruse-browser-node:latest"),
		PoolDockerNetwork:       envOrDefault("ORCHESTRATOR_POOL_DOCKER_NETWORK", "bridge"),
		PoolOrchestratorURL:     envOrDefault("ORCHESTRATOR_POOL_ORCHESTRATOR_URL", "http://host.docker.internal:8080"),
		PoolNodeVersion:         envOrDefault("ORCHESTRATOR_POOL_NODE_VERSION", "dev"),
		PoolNodeHeartbeat:       durationOrDefault("ORCHESTRATOR_POOL_NODE_HEARTBEAT_INTERVAL", 5*time.Second),
		PoolNodeRequestTimeout:  durationOrDefault("ORCHESTRATOR_POOL_NODE_REQUEST_TIMEOUT", 5*time.Second),
		PoolNodeCDPBaseURL:      envOrDefault("ORCHESTRATOR_POOL_NODE_CDP_BASE_URL", "http://127.0.0.1:9222"),
		PoolNodeRenderDelay:     durationOrDefault("ORCHESTRATOR_POOL_NODE_RENDER_DELAY", 2*time.Second),
		PoolNodeExecuteTimeout:  durationOrDefault("ORCHESTRATOR_POOL_NODE_EXECUTE_TIMEOUT", 45*time.Second),
		PoolNodePlannerMode:     envOrDefault("ORCHESTRATOR_POOL_NODE_PLANNER_MODE", "heuristic"),
		PoolXVFBScreenGeometry:  envOrDefault("ORCHESTRATOR_POOL_XVFB_SCREEN_GEOMETRY", "1280x720x24"),
		PoolChromeDebugPort:     intOrDefault("ORCHESTRATOR_POOL_CHROME_DEBUG_PORT", 9222),
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

func boolOrDefault(key string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
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
