package cdp

import (
	"context"
	"encoding/json"
	"errors"
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

const (
	defaultSelectorTimeout = 12 * time.Second
	pollInterval           = 150 * time.Millisecond
)

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
	conn.SetReadLimit(16 << 20)

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
	value, err := c.EvaluateAny(ctx, expression)
	if err != nil {
		return "", err
	}
	if value == nil {
		return "", nil
	}
	return fmt.Sprint(value), nil
}

func (c *Client) EvaluateAny(ctx context.Context, expression string) (any, error) {
	if err := c.Call(ctx, "Runtime.enable", nil, nil); err != nil {
		return nil, err
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
		return nil, err
	}
	return response.Result.Value, nil
}

func (c *Client) WaitForSelector(ctx context.Context, selector string, timeout time.Duration) error {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return errors.New("selector is required")
	}
	if timeout <= 0 {
		timeout = defaultSelectorTimeout
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	expression := fmt.Sprintf(`(() => {
	const visible = (el) => {
		const style = window.getComputedStyle(el);
		if (!style || style.display === "none" || style.visibility === "hidden") return false;
		const rect = el.getBoundingClientRect();
		return rect.width > 1 && rect.height > 1;
	};
	return Array.from(document.querySelectorAll(%q)).some(visible);
	})()`, selector)
	for {
		value, err := c.EvaluateAny(waitCtx, expression)
		if err != nil {
			return err
		}
		if found, ok := value.(bool); ok && found {
			return nil
		}

		select {
		case <-waitCtx.Done():
			return fmt.Errorf("timeout waiting for selector %q", selector)
		case <-time.After(pollInterval):
		}
	}
}

func (c *Client) WaitForURLContains(ctx context.Context, fragment string, timeout time.Duration) error {
	fragment = strings.TrimSpace(fragment)
	if fragment == "" {
		return errors.New("url fragment is required")
	}
	if timeout <= 0 {
		timeout = defaultSelectorTimeout
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	expression := fmt.Sprintf(`(() => {
	const needle = %q.toLowerCase();
	const href = String(window.location.href || "");
	let decoded = href;
	try {
		decoded = decodeURIComponent(href);
	} catch (_error) {}
	return href.toLowerCase().includes(needle) || decoded.toLowerCase().includes(needle);
	})()`, strings.ToLower(fragment))

	for {
		value, err := c.EvaluateAny(waitCtx, expression)
		if err != nil {
			return err
		}
		if matched, ok := value.(bool); ok && matched {
			return nil
		}

		select {
		case <-waitCtx.Done():
			return fmt.Errorf("timeout waiting for URL to contain %q", fragment)
		case <-time.After(pollInterval):
		}
	}
}

func (c *Client) ClickSelector(ctx context.Context, selector string) error {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return errors.New("selector is required")
	}

	expression := fmt.Sprintf(`(() => {
	const visible = (node) => {
		const style = window.getComputedStyle(node);
		if (!style || style.display === "none" || style.visibility === "hidden") return false;
		const rect = node.getBoundingClientRect();
		return rect.width > 1 && rect.height > 1;
	};
	const el = Array.from(document.querySelectorAll(%q)).find(visible);
	if (!el) return "not_found";
	el.scrollIntoView({block:"center", inline:"center"});
	if (typeof el.focus === "function") el.focus();
	el.click();
	return "ok";
	})()`, selector)

	result, err := c.EvaluateString(ctx, expression)
	if err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("click failed: %s", result)
	}
	return nil
}

func (c *Client) TypeIntoSelector(ctx context.Context, selector, text string) error {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return errors.New("selector is required")
	}

	expression := fmt.Sprintf(`(() => {
	const visible = (node) => {
		const style = window.getComputedStyle(node);
		if (!style || style.display === "none" || style.visibility === "hidden") return false;
		const rect = node.getBoundingClientRect();
		return rect.width > 1 && rect.height > 1;
	};
	const el = Array.from(document.querySelectorAll(%q)).find(visible);
	if (!el) return "not_found";
	el.scrollIntoView({block:"center", inline:"center"});
	el.focus();
	if ("value" in el) {
		el.value = "";
		el.dispatchEvent(new Event("input", {bubbles: true}));
	}
	return "ok";
	})()`, selector)

	result, err := c.EvaluateString(ctx, expression)
	if err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("type failed: %s", result)
	}
	if err := c.Call(ctx, "Input.insertText", map[string]any{"text": text}, nil); err != nil {
		return fmt.Errorf("type failed: insert text: %w", err)
	}
	return nil
}

func (c *Client) PressEnterOnSelector(ctx context.Context, selector string) error {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return errors.New("selector is required")
	}

	focusExpression := fmt.Sprintf(`(() => {
		const visible = (node) => {
			const style = window.getComputedStyle(node);
			if (!style || style.display === "none" || style.visibility === "hidden") return false;
			const rect = node.getBoundingClientRect();
			return rect.width > 1 && rect.height > 1;
		};
		const el = Array.from(document.querySelectorAll(%q)).find(visible);
		if (!el) return "not_found";
		el.scrollIntoView({block:"center", inline:"center"});
		el.focus();
		return "ok";
		})()`, selector)

	result, err := c.EvaluateString(ctx, focusExpression)
	if err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("press_enter failed: %s", result)
	}

	for _, eventType := range []string{"keyDown", "char", "keyUp"} {
		payload := map[string]any{
			"type":                  eventType,
			"key":                   "Enter",
			"code":                  "Enter",
			"windowsVirtualKeyCode": 13,
			"nativeVirtualKeyCode":  13,
		}
		if eventType == "char" {
			payload["text"] = "\r"
			payload["unmodifiedText"] = "\r"
		}
		if err := c.Call(ctx, "Input.dispatchKeyEvent", payload, nil); err != nil {
			return fmt.Errorf("dispatch enter %s: %w", eventType, err)
		}
	}

	return nil
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
