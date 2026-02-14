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
	"net/netip"
	"net/url"
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
	TraceScreenshots   bool
	HumanizeMode       string
	HumanizeSeed       int64
	EgressMode         string
	EgressAllowHosts   []string
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
	TraceID string          `json:"trace_id,omitempty"`
	URL     string          `json:"url"`
	Goal    string          `json:"goal"`
	Actions []executeAction `json:"actions,omitempty"`
}

// FlexInt unmarshals both JSON numbers and quoted-number strings (e.g. 5000 or "5000").
// LLMs frequently emit numeric values as strings.
type FlexInt int

func (f *FlexInt) UnmarshalJSON(b []byte) error {
	var n int
	if err := json.Unmarshal(b, &n); err == nil {
		*f = FlexInt(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		parsed, parseErr := strconv.Atoi(strings.TrimSpace(s))
		if parseErr != nil {
			return fmt.Errorf("FlexInt: cannot parse %q as int: %w", s, parseErr)
		}
		*f = FlexInt(parsed)
		return nil
	}
	return fmt.Errorf("FlexInt: unsupported value %s", string(b))
}

type executeAction struct {
	Type      string  `json:"type"`
	Selector  string  `json:"selector,omitempty"`
	Text      string  `json:"text,omitempty"`
	Pixels    FlexInt `json:"pixels,omitempty"`
	TimeoutMS FlexInt `json:"timeout_ms,omitempty"`
	DelayMS   FlexInt `json:"delay_ms,omitempty"`
}

type executeResponse struct {
	PageTitle        string             `json:"page_title"`
	FinalURL         string             `json:"final_url"`
	ScreenshotBase64 string             `json:"screenshot_base64"`
	BlockerType      string             `json:"blocker_type,omitempty"`
	BlockerMessage   string             `json:"blocker_message,omitempty"`
	Trace            []executeTraceStep `json:"trace,omitempty"`
}

type executeTraceStep struct {
	Index                 int           `json:"index"`
	Action                executeAction `json:"action"`
	Status                string        `json:"status"`
	Error                 string        `json:"error,omitempty"`
	OutputText            string        `json:"output_text,omitempty"`
	StartedAt             time.Time     `json:"started_at,omitempty"`
	CompletedAt           time.Time     `json:"completed_at,omitempty"`
	DurationMS            int64         `json:"duration_ms,omitempty"`
	ScreenshotBase64      string        `json:"screenshot_base64,omitempty"`
	ScreenshotArtifactURL string        `json:"screenshot_artifact_url,omitempty"`
}

type executeFlowError struct {
	message string
	result  executeResponse
}

func (e *executeFlowError) Error() string {
	return e.message
}

type browserExecutor struct {
	cdpBaseURL       string
	renderDelay      time.Duration
	executeTimeout   time.Duration
	planner          actionPlanner
	traceScreenshots bool
	humanizer        *humanizer
	egressMode       string
	egressAllowHosts []string
	mu               sync.Mutex
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
		traceScreenshots: cfg.TraceScreenshots,
		humanizer:        newHumanizer(cfg.HumanizeMode, cfg.HumanizeSeed),
		egressMode:       normalizeEgressMode(cfg.EgressMode),
		egressAllowHosts: append([]string(nil), cfg.EgressAllowHosts...),
	}
}

func (e *browserExecutor) Execute(ctx context.Context, targetURL string) (executeResponse, error) {
	return e.ExecuteWithActions(ctx, targetURL, "", nil, "")
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

func (e *browserExecutor) ExecuteWithActions(ctx context.Context, targetURL, goal string, actions []executeAction, traceID string) (executeResponse, error) {
	url := strings.TrimSpace(targetURL)
	if url == "" {
		return executeResponse{}, errors.New("url is required")
	}
	if err := validateEgressTarget(ctx, url, e.egressMode, e.egressAllowHosts); err != nil {
		return executeResponse{}, err
	}
	traceID = strings.TrimSpace(traceID)
	if traceID != "" {
		log.Printf("trace_id=%s node execution started task_url=%q", traceID, url)
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

	trace := make([]executeTraceStep, 0, len(actions))

	if blocked, response := e.detectBlocker(runCtx, client); blocked {
		response.Trace = append([]executeTraceStep(nil), trace...)
		return response, nil
	}

	executionActions := append([]executeAction(nil), actions...)
	if len(executionActions) == 0 && strings.TrimSpace(goal) != "" && e.planner != nil {
		snapshot, err := capturePageSnapshot(runCtx, client)
		if err != nil {
			if traceID != "" {
				log.Printf("trace_id=%s goal planning skipped (snapshot failed): goal=%q err=%v", traceID, goal, err)
			} else {
				log.Printf("goal planning skipped (snapshot failed): task goal=%q err=%v", goal, err)
			}
		} else {
			planned, err := e.planner.Plan(runCtx, goal, snapshot)
			if err != nil {
				if traceID != "" {
					log.Printf("trace_id=%s goal planning skipped (planner failed): planner=%s goal=%q err=%v", traceID, e.planner.Name(), goal, err)
				} else {
					log.Printf("goal planning skipped (planner failed): planner=%s goal=%q err=%v", e.planner.Name(), goal, err)
				}
			} else if len(planned) > 0 {
				executionActions = planned
				if traceID != "" {
					log.Printf("trace_id=%s planner=%s generated %d actions for goal=%q actions=%s", traceID, e.planner.Name(), len(planned), goal, summarizeActions(planned))
				} else {
					log.Printf("planner=%s generated %d actions for goal=%q actions=%s", e.planner.Name(), len(planned), goal, summarizeActions(planned))
				}
			}
		}
	}

	for index, action := range executionActions {
		started := time.Now().UTC()
		step := executeTraceStep{
			Index:     index + 1,
			Action:    normalizeTraceAction(action),
			Status:    "succeeded",
			StartedAt: started,
		}
		outputText, err := e.applyAction(runCtx, client, action)
		if err != nil {
			finished := time.Now().UTC()
			step.Status = "failed"
			step.Error = err.Error()
			step.CompletedAt = finished
			step.DurationMS = finished.Sub(started).Milliseconds()
			if screenshot, shotErr := client.CaptureScreenshot(runCtx); shotErr == nil {
				step.ScreenshotBase64 = screenshot
			}
			trace = append(trace, step)

			partial := executeResponse{
				Trace: append([]executeTraceStep(nil), trace...),
			}
			if title, evalErr := client.EvaluateString(runCtx, "document.title"); evalErr == nil {
				partial.PageTitle = title
			}
			if finalURL, evalErr := client.EvaluateString(runCtx, "window.location.href"); evalErr == nil {
				partial.FinalURL = finalURL
			}
			if screenshot, shotErr := client.CaptureScreenshot(runCtx); shotErr == nil {
				partial.ScreenshotBase64 = screenshot
				if traceIdx := len(trace) - 1; traceIdx >= 0 && strings.TrimSpace(trace[traceIdx].ScreenshotBase64) == "" {
					trace[traceIdx].ScreenshotBase64 = screenshot
					partial.Trace = append([]executeTraceStep(nil), trace...)
				}
			}
			return partial, &executeFlowError{
				message: fmt.Sprintf("action %d (%s) failed: %v", index+1, action.Type, err),
				result:  partial,
			}
		}
		finished := time.Now().UTC()
		step.CompletedAt = finished
		step.DurationMS = finished.Sub(started).Milliseconds()
		step.OutputText = strings.TrimSpace(outputText)
		if currentURL, evalErr := client.EvaluateString(runCtx, "window.location.href"); evalErr == nil {
			if policyErr := validateEgressTarget(runCtx, currentURL, e.egressMode, e.egressAllowHosts); policyErr != nil {
				step.Status = "failed"
				step.Error = policyErr.Error()
				trace = append(trace, step)
				return executeResponse{
						Trace: append([]executeTraceStep(nil), trace...),
					}, &executeFlowError{
						message: fmt.Sprintf("action %d (%s) failed: %v", index+1, action.Type, policyErr),
						result: executeResponse{
							Trace: append([]executeTraceStep(nil), trace...),
						},
					}
			}
		}
		if e.traceScreenshots {
			if screenshot, shotErr := client.CaptureScreenshot(runCtx); shotErr == nil {
				step.ScreenshotBase64 = screenshot
			}
		}
		trace = append(trace, step)
	}

	if blocked, response := e.detectBlocker(runCtx, client); blocked {
		response.Trace = append([]executeTraceStep(nil), trace...)
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
		Trace:            append([]executeTraceStep(nil), trace...),
	}, nil
}

func (e *browserExecutor) detectBlocker(ctx context.Context, client *cdp.Client) (bool, executeResponse) {
	title, finalURL, bodyText, err := collectBlockerSignals(ctx, client)
	if err != nil {
		return false, executeResponse{}
	}

	blockerType, blockerMessage := classifyBlocker(finalURL, title, bodyText)
	if blockerType == "" {
		return false, executeResponse{}
	}
	if blockerType == "human_verification_required" && isLikelyTransientChallenge(finalURL, title, bodyText) {
		recheckDelay := minDuration(3*time.Second, e.renderDelay)
		if recheckDelay <= 0 {
			recheckDelay = 2 * time.Second
		}
		select {
		case <-ctx.Done():
			return false, executeResponse{}
		case <-time.After(recheckDelay):
		}
		reTitle, reURL, reBody, reErr := collectBlockerSignals(ctx, client)
		if reErr == nil {
			title = reTitle
			finalURL = reURL
			bodyText = reBody
			blockerType, blockerMessage = classifyBlocker(finalURL, title, bodyText)
			if blockerType == "" {
				return false, executeResponse{}
			}
		}
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

func collectBlockerSignals(ctx context.Context, client *cdp.Client) (string, string, string, error) {
	title, err := client.EvaluateString(ctx, "document.title")
	if err != nil {
		return "", "", "", err
	}
	finalURL, err := client.EvaluateString(ctx, "window.location.href")
	if err != nil {
		return "", "", "", err
	}
	bodyText, err := client.EvaluateString(ctx, `(() => {
		const raw = document && document.body ? String(document.body.innerText || document.body.textContent || "") : "";
		return raw.replace(/\s+/g, " ").slice(0, 5000);
	})()`)
	if err != nil {
		return "", "", "", err
	}
	return title, finalURL, bodyText, nil
}

func (e *browserExecutor) applyAction(ctx context.Context, client *cdp.Client, action executeAction) (string, error) {
	actionType := strings.ToLower(strings.TrimSpace(action.Type))
	if actionType == "" {
		return "", errors.New("action type is required")
	}

	timeout := actionTimeout(int(action.TimeoutMS))
	actionCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	switch actionType {
	case "wait_for":
		selector := strings.TrimSpace(action.Selector)
		if selector == "" {
			return "", errors.New("selector is required for wait_for")
		}
		return "", client.WaitForSelector(actionCtx, selector, timeout)
	case "click":
		selector := strings.TrimSpace(action.Selector)
		if selector == "" {
			return "", errors.New("selector is required for click")
		}
		if err := client.WaitForSelector(actionCtx, selector, timeout); err != nil {
			return "", err
		}
		if e.humanizer != nil {
			if err := e.humanizer.Click(actionCtx, client, selector); err == nil {
				return "", nil
			} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return "", err
			} else {
				log.Printf("humanized click failed; falling back to deterministic click: selector=%q err=%v", selector, err)
			}
		}
		return "", client.ClickSelector(actionCtx, selector)
	case "type":
		selector := strings.TrimSpace(action.Selector)
		if selector == "" {
			return "", errors.New("selector is required for type")
		}
		if err := client.WaitForSelector(actionCtx, selector, timeout); err != nil {
			return "", err
		}
		typed := false
		if e.humanizer != nil {
			if err := e.humanizer.Type(actionCtx, client, selector, action.Text); err == nil {
				typed = true
			} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return "", err
			} else {
				log.Printf("humanized type failed; falling back to deterministic type: selector=%q err=%v", selector, err)
			}
		}
		if !typed {
			if err := client.TypeIntoSelector(actionCtx, selector, action.Text); err != nil {
				return "", err
			}
		}
		if strings.TrimSpace(action.Text) != "" {
			if err := client.WaitForSelectorValueContains(actionCtx, selector, action.Text, timeout); err != nil {
				return "", err
			}
		}
		if delay := boundedActionDelay(int(action.DelayMS), 0); delay > 0 {
			select {
			case <-actionCtx.Done():
				return "", actionCtx.Err()
			case <-time.After(delay):
			}
		}
		return "", nil
	case "scroll":
		direction := strings.TrimSpace(action.Text)
		if direction == "" {
			direction = "down"
		}
		scrolled := false
		if e.humanizer != nil {
			if err := e.humanizer.Scroll(actionCtx, client, direction, int(action.Pixels)); err == nil {
				scrolled = true
			} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return "", err
			} else {
				log.Printf("humanized scroll failed; falling back to deterministic scroll: direction=%q err=%v", direction, err)
			}
		}
		if !scrolled {
			if err := client.Scroll(actionCtx, direction, int(action.Pixels)); err != nil {
				return "", err
			}
		}
		if delay := boundedActionDelay(int(action.DelayMS), 0); delay > 0 {
			select {
			case <-actionCtx.Done():
				return "", actionCtx.Err()
			case <-time.After(delay):
			}
		}
		return "", nil
	case "extract_text":
		selector := strings.TrimSpace(action.Selector)
		if selector == "" {
			return "", errors.New("selector is required for extract_text")
		}
		candidates := splitExtractSelectorCandidates(selector)
		if len(candidates) == 0 {
			return "", errors.New("selector is required for extract_text")
		}
		expected := strings.ToLower(strings.TrimSpace(action.Text))
		perCandidateTimeout := timeout / time.Duration(len(candidates))
		if perCandidateTimeout < 1500*time.Millisecond {
			perCandidateTimeout = minDuration(timeout, 1500*time.Millisecond)
		}

		attemptErrors := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			if err := client.WaitForSelector(actionCtx, candidate, perCandidateTimeout); err != nil {
				attemptErrors = append(attemptErrors, fmt.Sprintf("%s: wait failed: %v", candidate, err))
				continue
			}
			text, err := client.ExtractText(actionCtx, candidate)
			if err != nil {
				attemptErrors = append(attemptErrors, fmt.Sprintf("%s: extract failed: %v", candidate, err))
				continue
			}
			normalized := strings.TrimSpace(text)
			if normalized == "" {
				attemptErrors = append(attemptErrors, fmt.Sprintf("%s: empty text", candidate))
				continue
			}
			if !isExtractedValueValid(expected, normalized) {
				attemptErrors = append(attemptErrors, fmt.Sprintf("%s: extracted text failed %s validation", candidate, expected))
				continue
			}
			return normalized, nil
		}

		if len(attemptErrors) == 0 {
			return "", errors.New("extract_text failed: no selectors were attempted")
		}
		return "", errors.New("extract_text failed: " + strings.Join(attemptErrors, " | "))
	case "wait":
		delay := boundedActionDelay(int(action.DelayMS), 500*time.Millisecond)
		select {
		case <-actionCtx.Done():
			return "", actionCtx.Err()
		case <-time.After(delay):
			return "", nil
		}
	case "press_enter":
		selector := strings.TrimSpace(action.Selector)
		if selector == "" {
			return "", errors.New("selector is required for press_enter")
		}
		if err := client.WaitForSelector(actionCtx, selector, timeout); err != nil {
			return "", err
		}
		previousURL, _ := client.EvaluateString(actionCtx, "window.location.href")
		if err := client.PressEnterOnSelector(actionCtx, selector); err != nil {
			return "", err
		}
		if strings.TrimSpace(previousURL) != "" {
			_, _ = client.WaitForURLChange(actionCtx, previousURL, minDuration(timeout, 1500*time.Millisecond))
		}
		return "", nil
	case "submit_search":
		selector := strings.TrimSpace(action.Selector)
		if selector == "" {
			return "", errors.New("selector is required for submit_search")
		}
		if err := client.WaitForSelector(actionCtx, selector, timeout); err != nil {
			return "", err
		}
		previousURL, _ := client.EvaluateString(actionCtx, "window.location.href")
		if err := client.PressEnterOnSelector(actionCtx, selector); err != nil {
			return "", err
		}
		if strings.TrimSpace(previousURL) != "" {
			_, _ = client.WaitForURLChange(actionCtx, previousURL, minDuration(timeout, 1500*time.Millisecond))
		}
		return "", nil
	case "wait_for_url_contains":
		fragment := strings.TrimSpace(action.Text)
		if fragment == "" {
			return "", errors.New("text is required for wait_for_url_contains")
		}
		return "", client.WaitForURLContains(actionCtx, fragment, timeout)
	default:
		return "", fmt.Errorf("unsupported action type %q", actionType)
	}
}

func actionTimeout(timeoutMS int) time.Duration {
	if timeoutMS <= 0 {
		return 12 * time.Second
	}
	if timeoutMS < 250 {
		timeoutMS = 250
	}
	if timeoutMS > 60000 {
		timeoutMS = 60000
	}
	return time.Duration(timeoutMS) * time.Millisecond
}

func boundedActionDelay(delayMS int, fallback time.Duration) time.Duration {
	if delayMS <= 0 {
		return fallback
	}
	delay := time.Duration(delayMS) * time.Millisecond
	if delay < 0 {
		return fallback
	}
	if delay > 15*time.Second {
		return 15 * time.Second
	}
	return delay
}

func splitExtractSelectorCandidates(selector string) []string {
	trimmed := strings.TrimSpace(selector)
	if trimmed == "" {
		return nil
	}
	raw := strings.Split(trimmed, "||")
	items := make([]string, 0, len(raw))
	for _, part := range raw {
		candidate := strings.TrimSpace(part)
		if candidate != "" {
			items = append(items, candidate)
		}
	}
	if len(items) == 0 {
		return []string{trimmed}
	}
	return items
}

func isExtractedValueValid(expected, value string) bool {
	trimmedExpected := strings.ToLower(strings.TrimSpace(expected))
	trimmedValue := strings.TrimSpace(value)
	if trimmedValue == "" {
		return false
	}
	if trimmedExpected == "" || trimmedExpected == "text" || trimmedExpected == "title" {
		return true
	}
	switch trimmedExpected {
	case "price":
		return looksLikePrice(trimmedValue)
	case "rating":
		return looksLikeRating(trimmedValue)
	default:
		return true
	}
}

func looksLikePrice(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	if lower == "" {
		return false
	}
	hasDigit := false
	for _, ch := range lower {
		if ch >= '0' && ch <= '9' {
			hasDigit = true
			break
		}
	}
	if !hasDigit {
		return false
	}
	for _, symbol := range []string{"$", "€", "£", "₹", "¥"} {
		if strings.Contains(lower, symbol) {
			return true
		}
	}
	for _, code := range []string{"usd", "eur", "gbp", "inr", "cad", "aud"} {
		if strings.Contains(lower, code) {
			return true
		}
	}
	return strings.Contains(lower, ".") || strings.Contains(lower, ",")
}

func looksLikeRating(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	if lower == "" {
		return false
	}
	hasDigit := false
	for _, ch := range lower {
		if ch >= '0' && ch <= '9' {
			hasDigit = true
			break
		}
	}
	if !hasDigit {
		return false
	}
	return strings.Contains(lower, "star") || strings.Contains(lower, "/5") || strings.Contains(lower, "out of")
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

func normalizeTraceAction(action executeAction) executeAction {
	return executeAction{
		Type:      strings.ToLower(strings.TrimSpace(action.Type)),
		Selector:  strings.TrimSpace(action.Selector),
		Text:      strings.TrimSpace(action.Text),
		Pixels:    action.Pixels,
		TimeoutMS: action.TimeoutMS,
		DelayMS:   action.DelayMS,
	}
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
		PlannerMode:        envOrDefault("NODE_AGENT_PLANNER_MODE", "template"),
		PlannerEndpoint:    strings.TrimSpace(os.Getenv("NODE_AGENT_PLANNER_ENDPOINT_URL")),
		PlannerAuthToken:   strings.TrimSpace(os.Getenv("NODE_AGENT_PLANNER_AUTH_TOKEN")),
		PlannerModel:       strings.TrimSpace(os.Getenv("NODE_AGENT_PLANNER_MODEL")),
		PlannerTimeout:     durationOrDefault("NODE_AGENT_PLANNER_TIMEOUT", 8*time.Second),
		PlannerMaxElements: intOrDefault("NODE_AGENT_PLANNER_MAX_ELEMENTS", 48),
		TraceScreenshots:   boolOrDefault("NODE_AGENT_TRACE_SCREENSHOTS", false),
		HumanizeMode:       envOrDefault("NODE_AGENT_HUMANIZE_MODE", "off"),
		HumanizeSeed:       int64OrDefault("NODE_AGENT_HUMANIZE_SEED", 0),
		EgressMode:         envOrDefault("NODE_AGENT_EGRESS_MODE", "open"),
		EgressAllowHosts:   parseCSV(os.Getenv("NODE_AGENT_EGRESS_ALLOW_HOSTS")),
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

		result, err := executor.ExecuteWithActions(r.Context(), req.URL, req.Goal, req.Actions, req.TraceID)
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

func normalizeEgressMode(raw string) string {
	mode := strings.ToLower(strings.TrimSpace(raw))
	switch mode {
	case "", "open":
		return "open"
	case "public_only":
		return "public_only"
	case "deny_all":
		return "deny_all"
	default:
		return "open"
	}
}

func validateEgressTarget(ctx context.Context, rawURL, mode string, allowHosts []string) error {
	mode = normalizeEgressMode(mode)
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil
	}
	if mode == "deny_all" {
		return fmt.Errorf("egress policy blocked navigation: mode=deny_all url=%q", trimmed)
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("invalid url %q: %w", trimmed, err)
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" {
		return fmt.Errorf("egress policy blocked navigation: empty host in url %q", trimmed)
	}
	if len(allowHosts) > 0 && !hostAllowed(host, allowHosts) {
		return fmt.Errorf("egress policy blocked host %q (not in NODE_AGENT_EGRESS_ALLOW_HOSTS)", host)
	}
	if mode != "public_only" {
		return nil
	}

	if isLocalHostname(host) {
		return fmt.Errorf("egress policy blocked local/private host %q", host)
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if isPrivateOrLocalIP(ip) {
			return fmt.Errorf("egress policy blocked local/private ip %q", host)
		}
		return nil
	}

	resolveCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupNetIP(resolveCtx, "ip", host)
	if err != nil {
		return fmt.Errorf("egress policy blocked host %q (dns lookup failed: %v)", host, err)
	}
	for _, ip := range addrs {
		if isPrivateOrLocalIP(ip) {
			return fmt.Errorf("egress policy blocked host %q resolved to local/private ip %s", host, ip.String())
		}
	}
	return nil
}

func hostAllowed(host string, allowHosts []string) bool {
	target := strings.ToLower(strings.TrimSpace(host))
	for _, item := range allowHosts {
		candidate := strings.ToLower(strings.TrimSpace(item))
		if candidate == "" {
			continue
		}
		if strings.HasPrefix(candidate, "*.") {
			suffix := strings.TrimPrefix(candidate, "*.")
			if target == suffix || strings.HasSuffix(target, "."+suffix) {
				return true
			}
			continue
		}
		if target == candidate || strings.HasSuffix(target, "."+candidate) {
			return true
		}
	}
	return false
}

func isLocalHostname(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "localhost", "localhost.localdomain":
		return true
	default:
		return false
	}
}

func isPrivateOrLocalIP(ip netip.Addr) bool {
	addr := ip.Unmap()
	if addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalMulticast() || addr.IsLinkLocalUnicast() || addr.IsMulticast() || addr.IsUnspecified() {
		return true
	}
	// Carrier-grade NAT range 100.64.0.0/10.
	cgnatPrefix := netip.MustParsePrefix("100.64.0.0/10")
	return cgnatPrefix.Contains(addr)
}

func parseCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
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

func int64OrDefault(key string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
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
