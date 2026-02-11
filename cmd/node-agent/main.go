package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

type config struct {
	HTTPAddr          string
	NodeID            string
	Version           string
	OrchestratorURL   string
	AdvertiseAddr     string
	HeartbeatInterval time.Duration
	RequestTimeout    time.Duration
}

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

func main() {
	cfg := loadConfig()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	httpServer := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      routes(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	go func() {
		log.Printf("node-agent listening on %s", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("node-agent http server failed: %v", err)
		}
	}()

	if cfg.OrchestratorURL == "" {
		log.Printf("NODE_AGENT_ORCHESTRATOR_URL not set, running health-only mode")
		<-ctx.Done()
		shutdownHTTP(httpServer)
		return
	}

	client := &http.Client{Timeout: cfg.RequestTimeout}
	bootedAt := time.Now().UTC()

	if err := registerLoop(ctx, client, cfg, bootedAt); err != nil {
		log.Fatalf("node registration failed: %v", err)
	}

	heartbeatTicker := time.NewTicker(cfg.HeartbeatInterval)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			shutdownHTTP(httpServer)
			return
		case <-heartbeatTicker.C:
			if err := sendHeartbeat(ctx, client, cfg); err != nil {
				log.Printf("heartbeat failed: %v", err)
			}
		}
	}
}

func loadConfig() config {
	httpAddr := envOrDefault("NODE_AGENT_HTTP_ADDR", ":8091")
	nodeID := strings.TrimSpace(os.Getenv("NODE_AGENT_NODE_ID"))
	if nodeID == "" {
		hostname, err := os.Hostname()
		if err != nil || strings.TrimSpace(hostname) == "" {
			nodeID = fmt.Sprintf("node-%d", time.Now().UnixNano())
		} else {
			nodeID = hostname
		}
	}

	advertise := strings.TrimSpace(os.Getenv("NODE_AGENT_ADVERTISE_ADDR"))
	if advertise == "" {
		advertise = guessAdvertiseAddr(httpAddr)
	}

	return config{
		HTTPAddr:          httpAddr,
		NodeID:            nodeID,
		Version:           envOrDefault("NODE_AGENT_VERSION", "dev"),
		OrchestratorURL:   strings.TrimSuffix(strings.TrimSpace(os.Getenv("NODE_AGENT_ORCHESTRATOR_URL")), "/"),
		AdvertiseAddr:     advertise,
		HeartbeatInterval: durationOrDefault("NODE_AGENT_HEARTBEAT_INTERVAL", 5*time.Second),
		RequestTimeout:    durationOrDefault("NODE_AGENT_REQUEST_TIMEOUT", 5*time.Second),
	}
}

func routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

func registerLoop(ctx context.Context, client *http.Client, cfg config, bootedAt time.Time) error {
	for {
		err := sendRegister(ctx, client, cfg, bootedAt)
		if err == nil {
			log.Printf("node registered: id=%s addr=%s", cfg.NodeID, cfg.AdvertiseAddr)
			return nil
		}
		log.Printf("register failed: %v", err)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func sendRegister(ctx context.Context, client *http.Client, cfg config, bootedAt time.Time) error {
	payload := registerNodeRequest{
		NodeID:  cfg.NodeID,
		Address: cfg.AdvertiseAddr,
		Version: cfg.Version,
		Booted:  bootedAt.Format(time.RFC3339),
	}
	return postJSON(ctx, client, cfg.OrchestratorURL+"/v1/nodes/register", payload)
}

func sendHeartbeat(ctx context.Context, client *http.Client, cfg config) error {
	payload := heartbeatNodeRequest{
		State:     "ready",
		Heartbeat: time.Now().UTC().Format(time.RFC3339),
	}
	url := fmt.Sprintf("%s/v1/nodes/%s/heartbeat", cfg.OrchestratorURL, cfg.NodeID)
	return postJSON(ctx, client, url, payload)
}

func postJSON(ctx context.Context, client *http.Client, url string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
}

func shutdownHTTP(server *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("node-agent shutdown error: %v", err)
	}
}

func guessAdvertiseAddr(httpAddr string) string {
	host, port, err := net.SplitHostPort(httpAddr)
	if err != nil {
		if strings.HasPrefix(httpAddr, ":") {
			port = strings.TrimPrefix(httpAddr, ":")
		} else {
			return httpAddr
		}
	}

	if host == "" || host == "0.0.0.0" || host == "::" {
		hostname, err := os.Hostname()
		if err != nil || strings.TrimSpace(hostname) == "" {
			host = "localhost"
		} else {
			host = hostname
		}
	}

	return net.JoinHostPort(host, port)
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
