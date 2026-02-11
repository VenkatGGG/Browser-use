package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"nhooyr.io/websocket"
)

type Client struct {
	conn      *websocket.Conn
	idCounter int64
	mu        sync.Mutex
}

type versionResponse struct {
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

type targetResponse struct {
	Type                 string `json:"type"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

type envelope struct {
	ID     int64           `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *responseError  `json:"error,omitempty"`
}

type responseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func Dial(ctx context.Context, baseURL string) (*Client, error) {
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		trimmed = "http://127.0.0.1:9222"
	}
	trimmed = strings.TrimSuffix(trimmed, "/")

	targetURL := trimmed + "/json/list"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build target request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query cdp target endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cdp target endpoint returned status %d", resp.StatusCode)
	}

	var targets []targetResponse
	if err := json.NewDecoder(resp.Body).Decode(&targets); err != nil {
		return nil, fmt.Errorf("decode cdp target response: %w", err)
	}

	var pageSocketURL string
	for _, target := range targets {
		if target.Type == "page" && strings.TrimSpace(target.WebSocketDebuggerURL) != "" {
			pageSocketURL = target.WebSocketDebuggerURL
			break
		}
	}
	if pageSocketURL == "" {
		return nil, fmt.Errorf("no page target websocket found")
	}

	conn, _, err := websocket.Dial(ctx, pageSocketURL, nil)
	if err != nil {
		return nil, fmt.Errorf("dial cdp websocket: %w", err)
	}

	return &Client{conn: conn}, nil
}

func (c *Client) Close() error {
	return c.conn.Close(websocket.StatusNormalClosure, "closing")
}

func (c *Client) Navigate(ctx context.Context, targetURL string) error {
	if err := c.Call(ctx, "Page.enable", nil, nil); err != nil {
		return err
	}
	return c.Call(ctx, "Page.navigate", map[string]any{"url": targetURL}, nil)
}

func (c *Client) CaptureScreenshot(ctx context.Context) (string, error) {
	if err := c.Call(ctx, "Page.enable", nil, nil); err != nil {
		return "", err
	}
	var response struct {
		Data string `json:"data"`
	}
	if err := c.Call(ctx, "Page.captureScreenshot", map[string]any{"format": "png"}, &response); err != nil {
		return "", err
	}
	return response.Data, nil
}

func (c *Client) EvaluateString(ctx context.Context, expression string) (string, error) {
	if err := c.Call(ctx, "Runtime.enable", nil, nil); err != nil {
		return "", err
	}
	var response struct {
		Result struct {
			Value any `json:"value"`
		} `json:"result"`
	}
	if err := c.Call(ctx, "Runtime.evaluate", map[string]any{
		"expression":    expression,
		"returnByValue": true,
	}, &response); err != nil {
		return "", err
	}
	if response.Result.Value == nil {
		return "", nil
	}
	return fmt.Sprint(response.Result.Value), nil
}

func (c *Client) Call(ctx context.Context, method string, params any, out any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.idCounter++
	requestID := c.idCounter

	payload := map[string]any{
		"id":     requestID,
		"method": method,
	}
	if params != nil {
		payload["params"] = params
	}

	deadline := time.Now().Add(20 * time.Second)
	if explicit, ok := ctx.Deadline(); ok {
		deadline = explicit
	}
	writeCtx, cancelWrite := context.WithDeadline(ctx, deadline)
	defer cancelWrite()
	if err := c.conn.Write(writeCtx, websocket.MessageText, mustMarshal(payload)); err != nil {
		return fmt.Errorf("write cdp request: %w", err)
	}

	for {
		readCtx, cancelRead := context.WithDeadline(ctx, deadline)
		_, message, err := c.conn.Read(readCtx)
		cancelRead()
		if err != nil {
			return fmt.Errorf("read cdp response: %w", err)
		}

		var env envelope
		if err := json.Unmarshal(message, &env); err != nil {
			continue
		}

		if env.ID != requestID {
			continue
		}

		if env.Error != nil {
			return fmt.Errorf("cdp %s failed (%d): %s", method, env.Error.Code, env.Error.Message)
		}

		if out != nil && len(env.Result) > 0 {
			if err := json.Unmarshal(env.Result, out); err != nil {
				return fmt.Errorf("decode %s response: %w", method, err)
			}
		}
		return nil
	}
}

func mustMarshal(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}
