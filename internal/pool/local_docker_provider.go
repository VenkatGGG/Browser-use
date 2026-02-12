package pool

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

type LocalDockerProviderConfig struct {
	Image              string
	Network            string
	ContainerPrefix    string
	NodeIDPrefix       string
	NodeGRPCPort       int
	NodeHTTPPort       int
	OrchestratorURL    string
	NodeVersion        string
	HeartbeatInterval  time.Duration
	RequestTimeout     time.Duration
	CDPBaseURL         string
	RenderDelay        time.Duration
	ExecuteTimeout     time.Duration
	PlannerMode        string
	PlannerEndpointURL string
	PlannerAuthToken   string
	PlannerModel       string
	PlannerTimeout     time.Duration
	PlannerMaxElements int
	TraceScreenshots   bool
	XVFBScreenGeometry string
	ChromeDebugPort    int
	DefaultMemoryLimit string
	DefaultCPULimit    string
	DefaultPIDsLimit   int
}

type LocalDockerProvider struct {
	cfg LocalDockerProviderConfig
}

func NewLocalDockerProvider(cfg LocalDockerProviderConfig) (*LocalDockerProvider, error) {
	if strings.TrimSpace(cfg.Image) == "" {
		return nil, fmt.Errorf("docker provider image is required")
	}
	if strings.TrimSpace(cfg.Network) == "" {
		cfg.Network = "bridge"
	}
	if strings.TrimSpace(cfg.ContainerPrefix) == "" {
		cfg.ContainerPrefix = "browseruse-node"
	}
	if strings.TrimSpace(cfg.NodeIDPrefix) == "" {
		cfg.NodeIDPrefix = "poolnode-"
	}
	if cfg.NodeGRPCPort <= 0 {
		cfg.NodeGRPCPort = 9091
	}
	if cfg.NodeHTTPPort <= 0 {
		cfg.NodeHTTPPort = 8091
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 5 * time.Second
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 5 * time.Second
	}
	if strings.TrimSpace(cfg.CDPBaseURL) == "" {
		cfg.CDPBaseURL = "http://127.0.0.1:9222"
	}
	if cfg.RenderDelay <= 0 {
		cfg.RenderDelay = 2 * time.Second
	}
	if cfg.ExecuteTimeout <= 0 {
		cfg.ExecuteTimeout = 45 * time.Second
	}
	if strings.TrimSpace(cfg.PlannerMode) == "" {
		cfg.PlannerMode = "template"
	}
	if cfg.PlannerTimeout <= 0 {
		cfg.PlannerTimeout = 8 * time.Second
	}
	if cfg.PlannerMaxElements <= 0 {
		cfg.PlannerMaxElements = 48
	}
	if strings.TrimSpace(cfg.XVFBScreenGeometry) == "" {
		cfg.XVFBScreenGeometry = "1280x720x24"
	}
	if cfg.ChromeDebugPort <= 0 {
		cfg.ChromeDebugPort = 9222
	}
	if strings.TrimSpace(cfg.DefaultMemoryLimit) == "" {
		cfg.DefaultMemoryLimit = "2g"
	}
	if strings.TrimSpace(cfg.DefaultCPULimit) == "" {
		cfg.DefaultCPULimit = "2.0"
	}
	if cfg.DefaultPIDsLimit <= 0 {
		cfg.DefaultPIDsLimit = 512
	}
	if strings.TrimSpace(cfg.OrchestratorURL) == "" {
		return nil, fmt.Errorf("docker provider orchestrator url is required")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker binary not found in PATH: %w", err)
	}
	return &LocalDockerProvider{cfg: cfg}, nil
}

func (p *LocalDockerProvider) ProvisionNode(ctx context.Context, input ProvisionInput) (Node, error) {
	nodeID := p.nextNodeID()
	containerName := p.containerName(nodeID)
	address := fmt.Sprintf("%s:%d", containerName, p.cfg.NodeGRPCPort)

	envVars := map[string]string{
		"NODE_AGENT_NODE_ID":              nodeID,
		"NODE_AGENT_ADVERTISE_ADDR":       address,
		"NODE_AGENT_ORCHESTRATOR_URL":     p.cfg.OrchestratorURL,
		"NODE_AGENT_VERSION":              p.cfg.NodeVersion,
		"NODE_AGENT_HTTP_ADDR":            fmt.Sprintf(":%d", p.cfg.NodeHTTPPort),
		"NODE_AGENT_GRPC_ADDR":            fmt.Sprintf(":%d", p.cfg.NodeGRPCPort),
		"NODE_AGENT_HEARTBEAT_INTERVAL":   p.cfg.HeartbeatInterval.String(),
		"NODE_AGENT_REQUEST_TIMEOUT":      p.cfg.RequestTimeout.String(),
		"NODE_AGENT_CDP_BASE_URL":         p.cfg.CDPBaseURL,
		"NODE_AGENT_RENDER_DELAY":         p.cfg.RenderDelay.String(),
		"NODE_AGENT_EXECUTE_TIMEOUT":      p.cfg.ExecuteTimeout.String(),
		"NODE_AGENT_PLANNER_MODE":         p.cfg.PlannerMode,
		"NODE_AGENT_PLANNER_TIMEOUT":      p.cfg.PlannerTimeout.String(),
		"NODE_AGENT_PLANNER_MAX_ELEMENTS": fmt.Sprintf("%d", p.cfg.PlannerMaxElements),
		"NODE_AGENT_TRACE_SCREENSHOTS":    fmt.Sprintf("%t", p.cfg.TraceScreenshots),
		"CHROME_DEBUG_PORT":               fmt.Sprintf("%d", p.cfg.ChromeDebugPort),
		"XVFB_SCREEN_GEOMETRY":            p.cfg.XVFBScreenGeometry,
	}
	if endpoint := strings.TrimSpace(p.cfg.PlannerEndpointURL); endpoint != "" {
		envVars["NODE_AGENT_PLANNER_ENDPOINT_URL"] = endpoint
	}
	if token := strings.TrimSpace(p.cfg.PlannerAuthToken); token != "" {
		envVars["NODE_AGENT_PLANNER_AUTH_TOKEN"] = token
	}
	if model := strings.TrimSpace(p.cfg.PlannerModel); model != "" {
		envVars["NODE_AGENT_PLANNER_MODEL"] = model
	}

	args := []string{
		"run", "-d",
		"--name", containerName,
		"--network", p.cfg.Network,
		"--read-only",
		"--tmpfs", "/tmp:size=1g,noexec,nosuid,nodev",
		"--tmpfs", "/home/nodeagent:size=256m,noexec,nosuid,nodev",
		"--tmpfs", "/var/log/browser-node:size=64m,noexec,nosuid,nodev",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges:true",
		"--pids-limit", fmt.Sprintf("%d", p.cfg.DefaultPIDsLimit),
		"--memory", p.cfg.DefaultMemoryLimit,
		"--cpus", p.cfg.DefaultCPULimit,
		"--label", "browseruse.managed=true",
		"--label", fmt.Sprintf("browseruse.node_id=%s", nodeID),
	}

	labelKeys := make([]string, 0, len(input.Labels))
	for key := range input.Labels {
		labelKeys = append(labelKeys, key)
	}
	sort.Strings(labelKeys)
	for _, key := range labelKeys {
		args = append(args, "--label", fmt.Sprintf("%s=%s", key, input.Labels[key]))
	}

	envKeys := make([]string, 0, len(envVars))
	for key := range envVars {
		envKeys = append(envKeys, key)
	}
	sort.Strings(envKeys)
	for _, key := range envKeys {
		args = append(args, "-e", key+"="+envVars[key])
	}

	args = append(args, p.cfg.Image)
	if _, err := p.runDocker(ctx, args...); err != nil {
		return Node{}, err
	}

	now := time.Now().UTC()
	return Node{
		ID:            nodeID,
		Address:       address,
		Version:       strings.TrimSpace(p.cfg.NodeVersion),
		State:         NodeStateWarming,
		BootedAt:      now,
		LastHeartbeat: time.Time{},
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}

func (p *LocalDockerProvider) DestroyNode(ctx context.Context, nodeID string) error {
	containerName := p.containerName(nodeID)
	_, err := p.runDocker(ctx, "rm", "-f", containerName)
	if err == nil {
		return nil
	}
	// Ignore not-found: node might have been deleted externally.
	if strings.Contains(err.Error(), "No such container") {
		return nil
	}
	return err
}

func (p *LocalDockerProvider) runDocker(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("docker %s failed: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (p *LocalDockerProvider) nextNodeID() string {
	raw := strings.ReplaceAll(uuid.NewString(), "-", "")
	return p.cfg.NodeIDPrefix + raw[:12]
}

func (p *LocalDockerProvider) containerName(nodeID string) string {
	safe := strings.NewReplacer(":", "-", "/", "-", " ", "-", "_", "-").Replace(strings.TrimSpace(nodeID))
	return p.cfg.ContainerPrefix + "-" + safe
}
