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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/VenkatGGG/Browser-use/internal/cdp"
	nodev1 "github.com/VenkatGGG/Browser-use/internal/gen"
	"github.com/VenkatGGG/Browser-use/pkg/httpx"
	"google.golang.org/grpc"
)

type config struct {
	HTTPAddr           string
	GRPCAddr           string
	NodeID             string
	Version            string
	OrchestratorURL    string
	AdvertiseAddr      string
	HeartbeatInterval  time.Duration
	RequestTimeout     time.Duration
	CDPBaseURL         string
	RenderDelay        time.Duration
	ExecuteTimeout     time.Duration
	PlannerMode        string
	PlannerEndpoint    string
	PlannerAuthToken   string
	PlannerModel       string
	PlannerTimeout     time.Duration
	PlannerMaxElements int
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

type executeRequest struct {
	TaskID  string          `json:"task_id"`
	URL     string          `json:"url"`
	Goal    string          `json:"goal"`
	Actions []executeAction `json:"actions,omitempty"`
}

type executeAction struct {
	Type      string `json:"type"`
	Selector  string `json:"selector,omitempty"`
	Text      string `json:"text,omitempty"`
	Pixels    int    `json:"pixels,omitempty"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
	DelayMS   int    `json:"delay_ms,omitempty"`
}

type executeResponse struct {
	PageTitle        string `json:"page_title"`
	FinalURL         string `json:"final_url"`
	ScreenshotBase64 string `json:"screenshot_base64"`
	BlockerType      string `json:"blocker_type,omitempty"`
	BlockerMessage   string `json:"blocker_message,omitempty"`
}

type browserExecutor struct {
	cdpBaseURL     string
	renderDelay    time.Duration
	executeTimeout time.Duration
	planner        actionPlanner
	mu             sync.Mutex
}

func newBrowserExecutor(cfg config) *browserExecutor {
	return &browserExecutor{
		cdpBaseURL:     cfg.CDPBaseURL,
		renderDelay:    cfg.RenderDelay,
		executeTimeout: cfg.ExecuteTimeout,
		planner: newActionPlanner(plannerConfig{
			Mode:        cfg.PlannerMode,
			EndpointURL: cfg.PlannerEndpoint,
			AuthToken:   cfg.PlannerAuthToken,
			Model:       cfg.PlannerModel,
			Timeout:     cfg.PlannerTimeout,
			MaxElements: cfg.PlannerMaxElements,
		}),
	}
}

func (e *browserExecutor) Execute(ctx context.Context, targetURL string) (executeResponse, error) {
	return e.ExecuteWithActions(ctx, targetURL, "", nil)
}

func (e *browserExecutor) dialWithRetry(ctx context.Context) (*cdp.Client, error) {
	var lastErr error
	for attempt := 1; attempt <= 20; attempt++ {
		client, err := cdp.Dial(ctx, e.cdpBaseURL)
		if err == nil {
			return client, nil
		}
		lastErr = err

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return nil, fmt.Errorf("dial cdp after retries: %w", lastErr)
}

func (e *browserExecutor) ExecuteWithActions(ctx context.Context, targetURL, goal string, actions []executeAction) (executeResponse, error) {
	url := strings.TrimSpace(targetURL)
	if url == "" {
		return executeResponse{}, errors.New("url is required")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	runCtx, cancel := context.WithTimeout(ctx, e.executeTimeout)
	defer cancel()

	client, err := e.dialWithRetry(runCtx)
	if err != nil {
		return executeResponse{}, err
	}
	defer client.Close()

	if err := client.Navigate(runCtx, url); err != nil {
		return executeResponse{}, err
	}

	if e.renderDelay > 0 {
		select {
		case <-runCtx.Done():
			return executeResponse{}, runCtx.Err()
		case <-time.After(e.renderDelay):
		}
	}

	if blocked, response := e.detectBlocker(runCtx, client); blocked {
		return response, nil
	}

	executionActions := append([]executeAction(nil), actions...)
	if len(executionActions) == 0 && strings.TrimSpace(goal) != "" && e.planner != nil {
		snapshot, err := capturePageSnapshot(runCtx, client)
		if err != nil {
			log.Printf("goal planning skipped (snapshot failed): task goal=%q err=%v", goal, err)
		} else {
			planned, err := e.planner.Plan(runCtx, goal, snapshot)
			if err != nil {
				log.Printf("goal planning skipped (planner failed): planner=%s goal=%q err=%v", e.planner.Name(), goal, err)
			} else if len(planned) > 0 {
				executionActions = planned
				log.Printf("planner=%s generated %d actions for goal=%q actions=%s", e.planner.Name(), len(planned), goal, summarizeActions(planned))
			}
		}
	}

	for index, action := range executionActions {
		if err := e.applyAction(runCtx, client, action); err != nil {
			return executeResponse{}, fmt.Errorf("action %d (%s) failed: %w", index+1, action.Type, err)
		}
	}

	if blocked, response := e.detectBlocker(runCtx, client); blocked {
		return response, nil
	}

	title, err := client.EvaluateString(runCtx, "document.title")
	if err != nil {
		return executeResponse{}, err
	}
	finalURL, err := client.EvaluateString(runCtx, "window.location.href")
	if err != nil {
		return executeResponse{}, err
	}
	screenshot, err := client.CaptureScreenshot(runCtx)
	if err != nil {
		return executeResponse{}, err
	}

	return executeResponse{
		PageTitle:        title,
		FinalURL:         finalURL,
		ScreenshotBase64: screenshot,
	}, nil
}

func (e *browserExecutor) detectBlocker(ctx context.Context, client *cdp.Client) (bool, executeResponse) {
	title, err := client.EvaluateString(ctx, "document.title")
	if err != nil {
		return false, executeResponse{}
	}
	finalURL, err := client.EvaluateString(ctx, "window.location.href")
	if err != nil {
		return false, executeResponse{}
	}
	bodyText, err := client.EvaluateString(ctx, `(() => {
		const raw = document && document.body ? String(document.body.innerText || document.body.textContent || "") : "";
		return raw.replace(/\s+/g, " ").slice(0, 5000);
	})()`)
	if err != nil {
		return false, executeResponse{}
	}

	blockerType, blockerMessage := classifyBlocker(finalURL, title, bodyText)
	if blockerType == "" {
		return false, executeResponse{}
	}

	screenshot := ""
	if shot, err := client.CaptureScreenshot(ctx); err == nil {
		screenshot = shot
	}
	if blockerMessage == "" {
		blockerMessage = "blocking challenge detected"
	}

	return true, executeResponse{
		PageTitle:        title,
		FinalURL:         finalURL,
		ScreenshotBase64: screenshot,
		BlockerType:      blockerType,
		BlockerMessage:   blockerMessage,
	}
}

func (e *browserExecutor) applyAction(ctx context.Context, client *cdp.Client, action executeAction) error {
	actionType := strings.ToLower(strings.TrimSpace(action.Type))
	if actionType == "" {
		return errors.New("action type is required")
	}

	timeout := actionTimeout(action.TimeoutMS)
	actionCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	switch actionType {
	case "wait_for":
		selector := strings.TrimSpace(action.Selector)
		if selector == "" {
			return errors.New("selector is required for wait_for")
		}
		return client.WaitForSelector(actionCtx, selector, timeout)
	case "click":
		selector := strings.TrimSpace(action.Selector)
		if selector == "" {
			return errors.New("selector is required for click")
		}
		if err := client.WaitForSelector(actionCtx, selector, timeout); err != nil {
			return err
		}
		return client.ClickSelector(actionCtx, selector)
	case "type":
		selector := strings.TrimSpace(action.Selector)
		if selector == "" {
			return errors.New("selector is required for type")
		}
		if err := client.WaitForSelector(actionCtx, selector, timeout); err != nil {
			return err
		}
		if err := client.TypeIntoSelector(actionCtx, selector, action.Text); err != nil {
			return err
		}
		if strings.TrimSpace(action.Text) != "" {
			if err := client.WaitForSelectorValueContains(actionCtx, selector, action.Text, timeout); err != nil {
				return err
			}
		}
		if action.DelayMS > 0 {
			delay := time.Duration(action.DelayMS) * time.Millisecond
			select {
			case <-actionCtx.Done():
				return actionCtx.Err()
			case <-time.After(delay):
			}
		}
		return nil
	case "scroll":
		direction := strings.TrimSpace(action.Text)
		if direction == "" {
			direction = "down"
		}
		if err := client.Scroll(actionCtx, direction, action.Pixels); err != nil {
			return err
		}
		if action.DelayMS > 0 {
			delay := time.Duration(action.DelayMS) * time.Millisecond
			select {
			case <-actionCtx.Done():
				return actionCtx.Err()
			case <-time.After(delay):
			}
		}
		return nil
	case "wait":
		delay := time.Duration(action.DelayMS) * time.Millisecond
		if delay <= 0 {
			delay = 500 * time.Millisecond
		}
		select {
		case <-actionCtx.Done():
			return actionCtx.Err()
		case <-time.After(delay):
			return nil
		}
	case "press_enter":
		selector := strings.TrimSpace(action.Selector)
		if selector == "" {
			return errors.New("selector is required for press_enter")
		}
		if err := client.WaitForSelector(actionCtx, selector, timeout); err != nil {
			return err
		}
		previousURL, _ := client.EvaluateString(actionCtx, "window.location.href")
		if err := client.PressEnterOnSelector(actionCtx, selector); err != nil {
			return err
		}
		if strings.TrimSpace(previousURL) != "" {
			_, _ = client.WaitForURLChange(actionCtx, previousURL, minDuration(timeout, 1500*time.Millisecond))
		}
		return nil
	case "submit_search":
		selector := strings.TrimSpace(action.Selector)
		if selector == "" {
			return errors.New("selector is required for submit_search")
		}
		if err := client.WaitForSelector(actionCtx, selector, timeout); err != nil {
			return err
		}
		previousURL, _ := client.EvaluateString(actionCtx, "window.location.href")
		if err := client.PressEnterOnSelector(actionCtx, selector); err != nil {
			return err
		}
		if strings.TrimSpace(previousURL) != "" {
			_, _ = client.WaitForURLChange(actionCtx, previousURL, minDuration(timeout, 1500*time.Millisecond))
		}
		return nil
	case "wait_for_url_contains":
		fragment := strings.TrimSpace(action.Text)
		if fragment == "" {
			return errors.New("text is required for wait_for_url_contains")
		}
		return client.WaitForURLContains(actionCtx, fragment, timeout)
	default:
		return fmt.Errorf("unsupported action type %q", actionType)
	}
}

func actionTimeout(timeoutMS int) time.Duration {
	if timeoutMS <= 0 {
		return 12 * time.Second
	}
	return time.Duration(timeoutMS) * time.Millisecond
}

func minDuration(a, b time.Duration) time.Duration {
	if a <= 0 {
		return b
	}
	if b <= 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}

func summarizeActions(actions []executeAction) string {
	if len(actions) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(actions))
	for _, action := range actions {
		label := strings.TrimSpace(action.Type)
		selector := strings.TrimSpace(action.Selector)
		if selector != "" {
			label += "(" + selector + ")"
		}
		parts = append(parts, label)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func main() {
	cfg := loadConfig()
	executor := newBrowserExecutor(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	grpcListener, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		log.Fatalf("node-agent grpc listen failed: %v", err)
	}
	grpcServer := grpc.NewServer()
	nodev1.RegisterNodeAgentServer(grpcServer, newGRPCNodeAgentServer(executor))
	go func() {
		log.Printf("node-agent gRPC listening on %s", cfg.GRPCAddr)
		if err := grpcServer.Serve(grpcListener); err != nil {
			log.Fatalf("node-agent grpc server failed: %v", err)
		}
	}()

	httpServer := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      routes(executor),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 90 * time.Second,
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
		shutdownGRPC(grpcServer)
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
			shutdownGRPC(grpcServer)
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
	grpcAddr := envOrDefault("NODE_AGENT_GRPC_ADDR", ":9091")
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
		advertise = guessAdvertiseAddr(grpcAddr)
	}

	return config{
		HTTPAddr:           httpAddr,
		GRPCAddr:           grpcAddr,
		NodeID:             nodeID,
		Version:            envOrDefault("NODE_AGENT_VERSION", "dev"),
		OrchestratorURL:    strings.TrimSuffix(strings.TrimSpace(os.Getenv("NODE_AGENT_ORCHESTRATOR_URL")), "/"),
		AdvertiseAddr:      advertise,
		HeartbeatInterval:  durationOrDefault("NODE_AGENT_HEARTBEAT_INTERVAL", 5*time.Second),
		RequestTimeout:     durationOrDefault("NODE_AGENT_REQUEST_TIMEOUT", 5*time.Second),
		CDPBaseURL:         envOrDefault("NODE_AGENT_CDP_BASE_URL", "http://127.0.0.1:9222"),
		RenderDelay:        durationOrDefault("NODE_AGENT_RENDER_DELAY", 2*time.Second),
		ExecuteTimeout:     durationOrDefault("NODE_AGENT_EXECUTE_TIMEOUT", 45*time.Second),
		PlannerMode:        envOrDefault("NODE_AGENT_PLANNER_MODE", "heuristic"),
		PlannerEndpoint:    strings.TrimSpace(os.Getenv("NODE_AGENT_PLANNER_ENDPOINT_URL")),
		PlannerAuthToken:   strings.TrimSpace(os.Getenv("NODE_AGENT_PLANNER_AUTH_TOKEN")),
		PlannerModel:       strings.TrimSpace(os.Getenv("NODE_AGENT_PLANNER_MODEL")),
		PlannerTimeout:     durationOrDefault("NODE_AGENT_PLANNER_TIMEOUT", 8*time.Second),
		PlannerMaxElements: intOrDefault("NODE_AGENT_PLANNER_MAX_ELEMENTS", 48),
	}
}

func routes(executor *browserExecutor) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/v1/execute", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}

		var req executeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
			return
		}

		result, err := executor.ExecuteWithActions(r.Context(), req.URL, req.Goal, req.Actions)
		if err != nil {
			httpx.WriteError(w, http.StatusBadGateway, "execution_failed", err.Error())
			return
		}
		httpx.WriteJSON(w, http.StatusOK, result)
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

func shutdownGRPC(server *grpc.Server) {
	done := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		server.Stop()
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

func intOrDefault(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
