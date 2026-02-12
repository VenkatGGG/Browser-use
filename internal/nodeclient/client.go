package nodeclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Action struct {
	Type      string `json:"type"`
	Selector  string `json:"selector,omitempty"`
	Text      string `json:"text,omitempty"`
	Pixels    int    `json:"pixels,omitempty"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
	DelayMS   int    `json:"delay_ms,omitempty"`
}

type StepTrace struct {
	Index                 int       `json:"index"`
	Action                Action    `json:"action"`
	Status                string    `json:"status"`
	Error                 string    `json:"error,omitempty"`
	OutputText            string    `json:"output_text,omitempty"`
	StartedAt             time.Time `json:"started_at,omitempty"`
	CompletedAt           time.Time `json:"completed_at,omitempty"`
	DurationMS            int64     `json:"duration_ms,omitempty"`
	ScreenshotBase64      string    `json:"screenshot_base64,omitempty"`
	ScreenshotArtifactURL string    `json:"screenshot_artifact_url,omitempty"`
}

type ExecuteInput struct {
	TaskID  string   `json:"task_id"`
	URL     string   `json:"url"`
	Goal    string   `json:"goal"`
	Actions []Action `json:"actions,omitempty"`
}

type ExecuteOutput struct {
	PageTitle        string      `json:"page_title"`
	FinalURL         string      `json:"final_url"`
	ScreenshotBase64 string      `json:"screenshot_base64"`
	BlockerType      string      `json:"blocker_type,omitempty"`
	BlockerMessage   string      `json:"blocker_message,omitempty"`
	Trace            []StepTrace `json:"trace,omitempty"`
}

type ExecutionError struct {
	Message string
	Output  ExecuteOutput
}

func (e *ExecutionError) Error() string {
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	return "node execution failed"
}

type Client interface {
	Execute(ctx context.Context, nodeAddress string, input ExecuteInput) (ExecuteOutput, error)
}

type HTTPClient struct {
	httpClient *http.Client
}

func NewHTTPClient(timeout time.Duration) *HTTPClient {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &HTTPClient{httpClient: &http.Client{Timeout: timeout}}
}

func (c *HTTPClient) Execute(ctx context.Context, nodeAddress string, input ExecuteInput) (ExecuteOutput, error) {
	if strings.TrimSpace(nodeAddress) == "" {
		return ExecuteOutput{}, fmt.Errorf("node address is required")
	}

	url := normalizeAddress(nodeAddress) + "/v1/execute"
	raw, err := json.Marshal(input)
	if err != nil {
		return ExecuteOutput{}, fmt.Errorf("marshal execute input: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return ExecuteOutput{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ExecuteOutput{}, fmt.Errorf("execute request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ExecuteOutput{}, fmt.Errorf("execute request returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var output ExecuteOutput
	if err := json.Unmarshal(body, &output); err != nil {
		return ExecuteOutput{}, fmt.Errorf("decode execute response: %w", err)
	}

	return output, nil
}

func normalizeAddress(nodeAddress string) string {
	trimmed := strings.TrimSpace(nodeAddress)
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return trimmed
	}
	return "http://" + trimmed
}
