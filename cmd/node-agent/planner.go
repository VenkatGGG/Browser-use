package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/VenkatGGG/Browser-use/internal/cdp"
)

type plannerConfig struct {
	Mode        string
	EndpointURL string
	AuthToken   string
	Model       string
	Timeout     time.Duration
	MaxElements int
}

type actionPlanner interface {
	Name() string
	Plan(ctx context.Context, goal string, snapshot pageSnapshot) ([]executeAction, error)
}

type heuristicPlanner struct{}
type templatePlanner struct {
	fallback actionPlanner
}
type endpointPlanner struct {
	endpointURL string
	authToken   string
	model       string
	timeout     time.Duration
	maxElements int
	client      *http.Client
	fallback    actionPlanner
}
type openAIPlanner struct {
	baseURL     string
	apiKey      string
	model       string
	timeout     time.Duration
	maxElements int
	client      *http.Client
	fallback    actionPlanner
}

type endpointPlanRequest struct {
	Goal           string             `json:"goal"`
	Model          string             `json:"model,omitempty"`
	AllowedActions []string           `json:"allowed_actions,omitempty"`
	State          plannerStatePacket `json:"state"`
}

type endpointPlanResponse struct {
	Actions []executeAction `json:"actions"`
	Plan    struct {
		Actions []executeAction `json:"actions"`
	} `json:"plan"`
}

type openAIChatRequest struct {
	Model       string              `json:"model"`
	Temperature float64             `json:"temperature,omitempty"`
	Messages    []openAIChatMessage `json:"messages"`
}

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func newActionPlanner(cfg plannerConfig) actionPlanner {
	normalized := strings.ToLower(strings.TrimSpace(cfg.Mode))
	heuristic := &heuristicPlanner{}
	template := &templatePlanner{fallback: heuristic}

	switch normalized {
	case "", "template":
		return template
	case "heuristic":
		return heuristic
	case "goal", "goal_template", "template_plus":
		return template
	case "endpoint", "llm", "hybrid":
		endpoint := strings.TrimSpace(cfg.EndpointURL)
		if endpoint == "" {
			return template
		}
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = 8 * time.Second
		}
		maxElements := cfg.MaxElements
		if maxElements <= 0 {
			maxElements = 48
		}
		return &endpointPlanner{
			endpointURL: endpoint,
			authToken:   strings.TrimSpace(cfg.AuthToken),
			model:       strings.TrimSpace(cfg.Model),
			timeout:     timeout,
			maxElements: maxElements,
			client:      &http.Client{},
			fallback:    template,
		}
	case "openai", "model":
		apiKey := strings.TrimSpace(cfg.AuthToken)
		if apiKey == "" {
			return template
		}
		baseURL := strings.TrimSpace(cfg.EndpointURL)
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1/chat/completions"
		}
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = 8 * time.Second
		}
		maxElements := cfg.MaxElements
		if maxElements <= 0 {
			maxElements = 48
		}
		model := strings.TrimSpace(cfg.Model)
		if model == "" {
			model = "gpt-4o-mini"
		}
		return &openAIPlanner{
			baseURL:     baseURL,
			apiKey:      apiKey,
			model:       model,
			timeout:     timeout,
			maxElements: maxElements,
			client:      &http.Client{},
			fallback:    template,
		}
	case "off":
		return nil
	default:
		return template
	}
}

func (p *heuristicPlanner) Name() string {
	return "heuristic"
}

func (p *templatePlanner) Name() string {
	return "template"
}

func (p *endpointPlanner) Name() string {
	return "endpoint"
}

func (p *openAIPlanner) Name() string {
	return "openai"
}

func (p *endpointPlanner) Plan(ctx context.Context, goal string, snapshot pageSnapshot) ([]executeAction, error) {
	endpoint := strings.TrimSpace(p.endpointURL)
	if endpoint == "" {
		return p.planFallback(ctx, goal, snapshot, errors.New("planner endpoint url is empty"))
	}

	reqPayload := endpointPlanRequest{
		Goal:           strings.TrimSpace(goal),
		Model:          strings.TrimSpace(p.model),
		AllowedActions: allowedPlannerActions(),
		State:          buildPlannerStatePacket(goal, snapshot, p.maxElements),
	}
	raw, err := json.Marshal(reqPayload)
	if err != nil {
		return p.planFallback(ctx, goal, snapshot, fmt.Errorf("marshal planner request: %w", err))
	}

	timeout := p.timeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return p.planFallback(ctx, goal, snapshot, fmt.Errorf("build planner request: %w", err))
	}
	req.Header.Set("Content-Type", "application/json")
	if token := strings.TrimSpace(p.authToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	httpClient := p.client
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return p.planFallback(ctx, goal, snapshot, fmt.Errorf("call planner endpoint: %w", err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return p.planFallback(ctx, goal, snapshot, fmt.Errorf("read planner endpoint response: %w", err))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return p.planFallback(ctx, goal, snapshot, fmt.Errorf("planner endpoint status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body))))
	}

	var decoded endpointPlanResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return p.planFallback(ctx, goal, snapshot, fmt.Errorf("decode planner endpoint response: %w", err))
	}

	actions := decoded.Actions
	if len(actions) == 0 && len(decoded.Plan.Actions) > 0 {
		actions = decoded.Plan.Actions
	}
	actions = sanitizePlannedActions(actions)
	if len(actions) == 0 {
		return p.planFallback(ctx, goal, snapshot, errors.New("planner endpoint returned no valid actions"))
	}
	return actions, nil
}

func (p *endpointPlanner) planFallback(ctx context.Context, goal string, snapshot pageSnapshot, cause error) ([]executeAction, error) {
	if p.fallback == nil {
		return nil, cause
	}
	log.Printf(
		"planner=%s fallback=%s goal=%q cause=%v",
		p.Name(),
		p.fallback.Name(),
		trimSnippet(strings.TrimSpace(goal), 120),
		cause,
	)
	actions, err := p.fallback.Plan(ctx, goal, snapshot)
	if err != nil {
		log.Printf("planner fallback failed: planner=%s fallback=%s err=%v", p.Name(), p.fallback.Name(), err)
		return nil, cause
	}
	log.Printf("planner fallback succeeded: planner=%s fallback=%s actions=%d", p.Name(), p.fallback.Name(), len(actions))
	return actions, nil
}

func (p *openAIPlanner) Plan(ctx context.Context, goal string, snapshot pageSnapshot) ([]executeAction, error) {
	baseURL := strings.TrimSpace(p.baseURL)
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1/chat/completions"
	}
	apiKey := strings.TrimSpace(p.apiKey)
	if apiKey == "" {
		return p.planFallback(ctx, goal, snapshot, errors.New("openai api key is empty"))
	}
	model := strings.TrimSpace(p.model)
	if model == "" {
		model = "gpt-4o-mini"
	}

	packet := buildPlannerStatePacket(goal, snapshot, p.maxElements)
	packetJSON, err := json.Marshal(packet)
	if err != nil {
		return p.planFallback(ctx, goal, snapshot, fmt.Errorf("encode planner state: %w", err))
	}

	reqPayload := openAIChatRequest{
		Model:       model,
		Temperature: 0.1,
		Messages: []openAIChatMessage{
			{
				Role: "system",
				Content: "You are a browser automation planner. Return ONLY valid JSON with this shape:\n" +
					"{\"actions\":[{\"type\":\"...\",\"selector\":\"...\",\"text\":\"...\",\"timeout_ms\":0,\"delay_ms\":0,\"pixels\":0}]}\n\n" +
					"Available action types and their required fields:\n" +
					"- wait_for: wait for element. Fields: selector (string, required), timeout_ms (int, optional, default 8000)\n" +
					"- click: click element. Fields: selector (string, required)\n" +
					"- type: type text into input. Fields: selector (string, required), text (string, required)\n" +
					"- press_enter: press enter on element. Fields: selector (string, required)\n" +
					"- scroll: scroll the page. Fields: text (string: \"down\" or \"up\"), pixels (int: number of pixels, e.g. 3000)\n" +
					"- wait: pause. Fields: delay_ms (int: milliseconds, e.g. 1000)\n" +
					"- extract_text: get text from element. Fields: selector (string, required)\n" +
					"- wait_for_url_contains: wait for URL change. Fields: text (string: substring to match), timeout_ms (int)\n\n" +
					"IMPORTANT: pixels, timeout_ms, and delay_ms must be integers, NOT strings. " +
					"Do not include explanations. Return ONLY JSON.",
			},
			{
				Role:    "user",
				Content: "Goal:\n" + strings.TrimSpace(goal) + "\n\nState packet JSON:\n" + string(packetJSON),
			},
		},
	}
	raw, err := json.Marshal(reqPayload)
	if err != nil {
		return p.planFallback(ctx, goal, snapshot, fmt.Errorf("encode openai request: %w", err))
	}

	timeout := p.timeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, baseURL, bytes.NewReader(raw))
	if err != nil {
		return p.planFallback(ctx, goal, snapshot, fmt.Errorf("build openai request: %w", err))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	httpClient := p.client
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return p.planFallback(ctx, goal, snapshot, fmt.Errorf("call openai planner: %w", err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return p.planFallback(ctx, goal, snapshot, fmt.Errorf("read openai response: %w", err))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return p.planFallback(ctx, goal, snapshot, fmt.Errorf("openai planner status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body))))
	}

	var decoded openAIChatResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return p.planFallback(ctx, goal, snapshot, fmt.Errorf("decode openai response: %w", err))
	}
	if len(decoded.Choices) == 0 {
		return p.planFallback(ctx, goal, snapshot, errors.New("openai planner returned no choices"))
	}

	content := strings.TrimSpace(decoded.Choices[0].Message.Content)
	if content == "" {
		return p.planFallback(ctx, goal, snapshot, errors.New("openai planner returned empty content"))
	}
	content = extractJSONObject(content)

	var parsed endpointPlanResponse
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return p.planFallback(ctx, goal, snapshot, fmt.Errorf("decode openai actions payload: %w", err))
	}

	actions := parsed.Actions
	if len(actions) == 0 && len(parsed.Plan.Actions) > 0 {
		actions = parsed.Plan.Actions
	}
	actions = sanitizePlannedActions(actions)
	if len(actions) == 0 {
		return p.planFallback(ctx, goal, snapshot, errors.New("openai planner returned no valid actions"))
	}
	return actions, nil
}

func (p *openAIPlanner) planFallback(ctx context.Context, goal string, snapshot pageSnapshot, cause error) ([]executeAction, error) {
	if p.fallback == nil {
		return nil, cause
	}
	log.Printf(
		"planner=%s fallback=%s goal=%q cause=%v",
		p.Name(),
		p.fallback.Name(),
		trimSnippet(strings.TrimSpace(goal), 120),
		cause,
	)
	actions, err := p.fallback.Plan(ctx, goal, snapshot)
	if err != nil {
		log.Printf("planner fallback failed: planner=%s fallback=%s err=%v", p.Name(), p.fallback.Name(), err)
		return nil, cause
	}
	log.Printf("planner fallback succeeded: planner=%s fallback=%s actions=%d", p.Name(), p.fallback.Name(), len(actions))
	return actions, nil
}

func extractJSONObject(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return trimmed
	}

	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	trimmed = strings.TrimSpace(trimmed)

	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		return strings.TrimSpace(trimmed[start : end+1])
	}
	return trimmed
}

func (p *heuristicPlanner) Plan(_ context.Context, goal string, snapshot pageSnapshot) ([]executeAction, error) {
	query := parseSearchQuery(goal)
	if query == "" {
		return nil, nil
	}

	inputSelector := selectSearchInput(snapshot.Elements)
	if inputSelector == "" {
		return nil, nil
	}

	actions := []executeAction{
		{Type: "wait_for", Selector: inputSelector, TimeoutMS: 8000},
		{Type: "type", Selector: inputSelector, Text: query},
		{Type: "press_enter", Selector: inputSelector},
		{Type: "wait_for_url_contains", Text: query, TimeoutMS: 10000},
		{Type: "wait", DelayMS: 800},
	}
	return actions, nil
}

func (p *templatePlanner) Plan(ctx context.Context, goal string, snapshot pageSnapshot) ([]executeAction, error) {
	trimmedGoal := strings.TrimSpace(goal)
	if trimmedGoal == "" {
		if p.fallback == nil {
			return nil, nil
		}
		return p.fallback.Plan(ctx, goal, snapshot)
	}

	query := parseSearchQuery(trimmedGoal)
	host := snapshotHost(snapshot.URL)
	priceIntent := isPriceExtractionGoal(trimmedGoal)
	extractIntent := isTextExtractionGoal(trimmedGoal)

	actions := make([]executeAction, 0, 6)
	if query != "" {
		inputSelector := selectSearchInput(snapshot.Elements)
		if inputSelector != "" {
			actions = append(actions,
				executeAction{Type: "wait_for", Selector: inputSelector, TimeoutMS: 8000},
				executeAction{Type: "type", Selector: inputSelector, Text: query},
				executeAction{Type: "press_enter", Selector: inputSelector},
				executeAction{Type: "wait", DelayMS: 1200},
			)
		}
	}

	if priceIntent {
		selector := priceExtractionSelector(host)
		if selector != "" {
			actions = append(actions, executeAction{
				Type:      "extract_text",
				Selector:  selector,
				Text:      "price",
				TimeoutMS: 10000,
			})
		}
	} else if extractIntent {
		selector := genericExtractionSelector(host, trimmedGoal)
		if selector != "" {
			actions = append(actions, executeAction{
				Type:      "extract_text",
				Selector:  selector,
				Text:      extractionHintFromGoal(trimmedGoal),
				TimeoutMS: 10000,
			})
		}
	}

	if len(actions) > 0 {
		return actions, nil
	}
	if p.fallback == nil {
		return nil, nil
	}
	return p.fallback.Plan(ctx, goal, snapshot)
}

type pageSnapshot struct {
	URL            string        `json:"url"`
	Title          string        `json:"title"`
	ViewportWidth  int           `json:"viewport_width"`
	ViewportHeight int           `json:"viewport_height"`
	ScrollX        int           `json:"scroll_x"`
	ScrollY        int           `json:"scroll_y"`
	Elements       []pageElement `json:"elements"`
}

type pageElement struct {
	StableID    string `json:"stable_id"`
	Tag         string `json:"tag"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	ID          string `json:"id"`
	Placeholder string `json:"placeholder"`
	AriaLabel   string `json:"aria_label"`
	Role        string `json:"role"`
	Text        string `json:"text"`
	Selector    string `json:"selector"`
	X           int    `json:"x"`
	Y           int    `json:"y"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
}

type plannerStatePacket struct {
	URL      string                `json:"url"`
	Title    string                `json:"title"`
	Goal     string                `json:"goal"`
	Viewport plannerViewport       `json:"viewport"`
	Elements []plannerStateElement `json:"elements"`
}

type plannerViewport struct {
	Width   int `json:"width"`
	Height  int `json:"height"`
	ScrollX int `json:"scroll_x"`
	ScrollY int `json:"scroll_y"`
}

type plannerStateElement struct {
	ID       string `json:"id"`
	Role     string `json:"role"`
	Name     string `json:"name,omitempty"`
	Text     string `json:"text,omitempty"`
	Selector string `json:"selector"`
	X        int    `json:"x"`
	Y        int    `json:"y"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

func capturePageSnapshot(ctx context.Context, client *cdp.Client) (pageSnapshot, error) {
	const expression = `(() => {
  const visible = (el) => {
    const style = window.getComputedStyle(el);
    if (!style || style.visibility === "hidden" || style.display === "none") return false;
    const rect = el.getBoundingClientRect();
    return rect.width > 2 && rect.height > 2;
  };

  const toText = (value) => String(value || "").trim().replace(/\s+/g, " ").slice(0, 120);
  const cssEscape = (value) => {
    if (typeof CSS !== "undefined" && typeof CSS.escape === "function") return CSS.escape(String(value));
    return String(value).replace(/["\\]/g, "\\$&");
  };
  const selectorSegment = (el) => {
    const tag = (el.tagName || "div").toLowerCase();
    if (el.id) return tag + "#" + cssEscape(el.id);
    let index = 1;
    let sibling = el;
    while ((sibling = sibling.previousElementSibling)) {
      if ((sibling.tagName || "").toLowerCase() === tag) index++;
    }
    return tag + ":nth-of-type(" + index + ")";
  };
  const selectorFor = (el) => {
    if (el.id) {
      const byID = "#" + cssEscape(el.id);
      if (document.querySelectorAll(byID).length === 1) return byID;
    }
    const parts = [];
    let current = el;
    while (current && current.nodeType === 1 && parts.length < 8) {
      parts.unshift(selectorSegment(current));
      const selector = parts.join(" > ");
      if (document.querySelectorAll(selector).length === 1) return selector;
      if (current.id) break;
      current = current.parentElement;
    }
    return parts.join(" > ");
  };

  const nodes = [];
  const seen = new Set();
  const selectors = "input,textarea,select,button,a,[role='button'],[contenteditable='true']";
	  for (const el of document.querySelectorAll(selectors)) {
	    if (!visible(el)) continue;
	    const rect = el.getBoundingClientRect();
	    const selector = selectorFor(el);
	    const key = selector + "|" + toText(el.innerText || el.textContent);
	    if (seen.has(key)) continue;
	    seen.add(key);
	    nodes.push({
      tag: (el.tagName || "").toLowerCase(),
      type: toText(el.type || ""),
      name: toText(el.name || ""),
      id: toText(el.id || ""),
      placeholder: toText(el.placeholder || ""),
	      aria_label: toText(el.getAttribute("aria-label") || ""),
	      role: toText(el.getAttribute("role") || ""),
	      text: toText(el.innerText || el.textContent || ""),
	      selector,
	      x: Math.round(rect.left),
	      y: Math.round(rect.top),
	      width: Math.round(rect.width),
	      height: Math.round(rect.height)
	    });
    if (nodes.length >= 80) break;
  }

  return {
    url: String(window.location.href || ""),
    title: String(document.title || ""),
    viewport_width: Math.round(window.innerWidth || 0),
    viewport_height: Math.round(window.innerHeight || 0),
    scroll_x: Math.round(window.scrollX || 0),
    scroll_y: Math.round(window.scrollY || 0),
    elements: nodes
  };
})()`

	raw, err := client.EvaluateAny(ctx, expression)
	if err != nil {
		return pageSnapshot{}, fmt.Errorf("collect page snapshot: %w", err)
	}

	payload, err := json.Marshal(raw)
	if err != nil {
		return pageSnapshot{}, fmt.Errorf("encode page snapshot: %w", err)
	}

	var snapshot pageSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return pageSnapshot{}, fmt.Errorf("decode page snapshot: %w", err)
	}
	assignStableElementIDs(&snapshot)
	return snapshot, nil
}

func assignStableElementIDs(snapshot *pageSnapshot) {
	for i := range snapshot.Elements {
		snapshot.Elements[i].StableID = stableIDForElement(snapshot.Elements[i])
	}
}

func stableIDForElement(element pageElement) string {
	parts := []string{
		strings.ToLower(strings.TrimSpace(element.Tag)),
		strings.ToLower(strings.TrimSpace(element.Type)),
		strings.ToLower(strings.TrimSpace(element.Name)),
		strings.ToLower(strings.TrimSpace(element.ID)),
		strings.ToLower(strings.TrimSpace(element.Placeholder)),
		strings.ToLower(strings.TrimSpace(element.AriaLabel)),
		strings.ToLower(strings.TrimSpace(element.Role)),
		strings.ToLower(strings.TrimSpace(element.Text)),
		strings.ToLower(strings.TrimSpace(element.Selector)),
		fmt.Sprintf("%d", element.X),
		fmt.Sprintf("%d", element.Y),
		fmt.Sprintf("%d", element.Width),
		fmt.Sprintf("%d", element.Height),
	}
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(strings.Join(parts, "|")))
	return fmt.Sprintf("el_%08x", hasher.Sum32())
}

func buildPlannerStatePacket(goal string, snapshot pageSnapshot, maxElements int) plannerStatePacket {
	limit := maxElements
	if limit <= 0 {
		limit = 48
	}
	if limit > 120 {
		limit = 120
	}

	elements := make([]plannerStateElement, 0, minInt(len(snapshot.Elements), limit))
	for _, element := range snapshot.Elements {
		if len(elements) >= limit {
			break
		}

		selector := strings.TrimSpace(element.Selector)
		if selector == "" {
			continue
		}

		role := firstNonEmptyString(strings.TrimSpace(element.Role), strings.TrimSpace(element.Type), strings.TrimSpace(element.Tag))
		role = strings.ToLower(strings.TrimSpace(role))
		if role == "" {
			role = "interactive"
		}

		name := firstNonEmptyString(
			strings.TrimSpace(element.AriaLabel),
			strings.TrimSpace(element.Name),
			strings.TrimSpace(element.Placeholder),
			strings.TrimSpace(element.ID),
		)

		elements = append(elements, plannerStateElement{
			ID:       firstNonEmptyString(strings.TrimSpace(element.StableID), stableIDForElement(element)),
			Role:     trimSnippet(role, 32),
			Name:     trimSnippet(name, 120),
			Text:     trimSnippet(strings.TrimSpace(element.Text), 160),
			Selector: trimSnippet(selector, 220),
			X:        element.X,
			Y:        element.Y,
			Width:    element.Width,
			Height:   element.Height,
		})
	}

	return plannerStatePacket{
		URL:   strings.TrimSpace(snapshot.URL),
		Title: trimSnippet(strings.TrimSpace(snapshot.Title), 160),
		Goal:  trimSnippet(strings.TrimSpace(goal), 220),
		Viewport: plannerViewport{
			Width:   snapshot.ViewportWidth,
			Height:  snapshot.ViewportHeight,
			ScrollX: snapshot.ScrollX,
			ScrollY: snapshot.ScrollY,
		},
		Elements: elements,
	}
}

func allowedPlannerActions() []string {
	return []string{
		"wait_for",
		"click",
		"type",
		"scroll",
		"extract_text",
		"wait",
		"press_enter",
		"submit_search",
		"wait_for_url_contains",
	}
}

func sanitizePlannedActions(actions []executeAction) []executeAction {
	if len(actions) == 0 {
		return nil
	}
	allowed := map[string]struct{}{
		"wait_for":              {},
		"click":                 {},
		"type":                  {},
		"scroll":                {},
		"extract_text":          {},
		"wait":                  {},
		"press_enter":           {},
		"submit_search":         {},
		"wait_for_url_contains": {},
	}

	cleaned := make([]executeAction, 0, len(actions))
	seen := make(map[string]struct{}, len(actions))
	for _, action := range actions {
		typ := strings.ToLower(strings.TrimSpace(action.Type))
		if _, ok := allowed[typ]; !ok {
			continue
		}

		normalized := executeAction{
			Type:      typ,
			Selector:  strings.TrimSpace(action.Selector),
			Text:      trimSnippet(strings.TrimSpace(action.Text), 320),
			TimeoutMS: action.TimeoutMS,
			DelayMS:   action.DelayMS,
			Pixels:    action.Pixels,
		}

		if normalized.TimeoutMS < 0 {
			normalized.TimeoutMS = 0
		}
		if normalized.TimeoutMS > 60000 {
			normalized.TimeoutMS = 60000
		}
		if normalized.DelayMS < 0 {
			normalized.DelayMS = 0
		}
		if normalized.DelayMS > 15000 {
			normalized.DelayMS = 15000
		}
		if normalized.Pixels < 0 {
			normalized.Pixels = 0
		}
		if normalized.Pixels > 3000 {
			normalized.Pixels = 3000
		}

		switch typ {
		case "wait_for", "click", "type", "extract_text", "press_enter", "submit_search":
			if normalized.Selector == "" {
				continue
			}
			if typ == "type" && normalized.Text == "" {
				continue
			}
		case "wait_for_url_contains":
			if len(normalized.Text) < 2 {
				continue
			}
		case "scroll":
			direction := strings.ToLower(strings.TrimSpace(normalized.Text))
			if direction != "up" && direction != "down" {
				normalized.Text = "down"
			} else {
				normalized.Text = direction
			}
		}

		signature := strings.Join([]string{
			normalized.Type,
			normalized.Selector,
			normalized.Text,
			fmt.Sprintf("%d", normalized.TimeoutMS),
			fmt.Sprintf("%d", normalized.DelayMS),
			fmt.Sprintf("%d", normalized.Pixels),
		}, "|")
		if _, duplicate := seen[signature]; duplicate {
			continue
		}
		seen[signature] = struct{}{}

		cleaned = append(cleaned, normalized)
		if len(cleaned) >= 12 {
			break
		}
	}

	return cleaned
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func trimSnippet(value string, max int) string {
	normalized := spacesExpr.ReplaceAllString(strings.TrimSpace(value), " ")
	if max <= 0 || len(normalized) <= max {
		return normalized
	}
	return strings.TrimSpace(normalized[:max])
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func snapshotHost(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parsed.Hostname()))
}

func isPriceExtractionGoal(goal string) bool {
	lower := strings.ToLower(strings.TrimSpace(goal))
	if lower == "" {
		return false
	}
	priceTerms := []string{
		"price",
		"cost",
		"how much",
		"amount",
	}
	for _, term := range priceTerms {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

func isTextExtractionGoal(goal string) bool {
	lower := strings.ToLower(strings.TrimSpace(goal))
	if lower == "" {
		return false
	}
	terms := []string{
		"extract",
		"get me",
		"tell me",
		"find the",
		"what is",
	}
	for _, term := range terms {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

func priceExtractionSelector(host string) string {
	switch {
	case strings.Contains(host, "amazon."):
		return "span.a-price span.a-offscreen || span[data-a-color='price'] span.a-offscreen || span.a-price-whole"
	case strings.Contains(host, "flipkart."):
		return "div.Nx9bqj || div._30jeq3 || div._16Jk6d"
	case strings.Contains(host, "ebay."):
		return "div.x-price-primary span.ux-textspans || span[itemprop='price']"
	default:
		return "[itemprop='price'] || [data-testid*='price'] || .price || .product-price || .a-price .a-offscreen"
	}
}

func genericExtractionSelector(host, goal string) string {
	lowerGoal := strings.ToLower(strings.TrimSpace(goal))
	switch {
	case strings.Contains(lowerGoal, "title"), strings.Contains(lowerGoal, "name"):
		if strings.Contains(host, "amazon.") {
			return "span#productTitle || h2 span || h1"
		}
		return "h1 || h2 || [data-testid*='title'] || [itemprop='name']"
	case strings.Contains(lowerGoal, "rating"), strings.Contains(lowerGoal, "stars"), strings.Contains(lowerGoal, "review"):
		if strings.Contains(host, "amazon.") {
			return "span[data-hook='rating-out-of-text'] || i.a-icon-star-small span.a-icon-alt || span.a-size-base.s-underline-text"
		}
		return "[itemprop='ratingValue'] || [data-testid*='rating'] || [class*='rating']"
	default:
		return ""
	}
}

func extractionHintFromGoal(goal string) string {
	lower := strings.ToLower(strings.TrimSpace(goal))
	switch {
	case strings.Contains(lower, "price"), strings.Contains(lower, "cost"), strings.Contains(lower, "how much"), strings.Contains(lower, "amount"):
		return "price"
	case strings.Contains(lower, "rating"), strings.Contains(lower, "stars"), strings.Contains(lower, "review"):
		return "rating"
	case strings.Contains(lower, "title"), strings.Contains(lower, "name"):
		return "title"
	default:
		return "text"
	}
}

var (
	spacesExpr = regexp.MustCompile(`\s+`)
	trimExpr   = regexp.MustCompile(`^[\s"'` + "`" + `]+|[\s"'` + "`" + `.,!?;:]+$`)
)

func parseSearchQuery(goal string) string {
	trimmed := strings.TrimSpace(goal)
	if trimmed == "" {
		return ""
	}

	lower := strings.ToLower(trimmed)
	patterns := []string{
		"search for ",
		"search ",
		"find ",
		"look up ",
		"lookup ",
	}
	for _, pattern := range patterns {
		index := strings.Index(lower, pattern)
		if index < 0 {
			continue
		}
		value := strings.TrimSpace(trimmed[index+len(pattern):])
		value = trimExpr.ReplaceAllString(value, "")
		value = spacesExpr.ReplaceAllString(value, " ")
		return strings.TrimSpace(value)
	}
	return ""
}

func selectSearchInput(elements []pageElement) string {
	type candidate struct {
		element pageElement
		score   int
		area    int
	}

	candidates := make([]candidate, 0, len(elements))
	for _, element := range elements {
		tag := strings.ToLower(strings.TrimSpace(element.Tag))
		if tag != "input" && tag != "textarea" {
			continue
		}
		if strings.TrimSpace(element.Selector) == "" {
			continue
		}

		typ := strings.ToLower(strings.TrimSpace(element.Type))
		if typ == "hidden" || typ == "checkbox" || typ == "radio" {
			continue
		}

		score := 0
		if typ == "search" {
			score += 8
		}
		if typ == "text" || typ == "" {
			score += 2
		}

		haystack := strings.ToLower(strings.Join([]string{
			element.Name,
			element.Placeholder,
			element.AriaLabel,
			element.Text,
			element.ID,
		}, " "))

		if strings.Contains(haystack, "search") {
			score += 6
		}
		if strings.Contains(haystack, "query") || strings.Contains(haystack, "keyword") {
			score += 4
		}
		if strings.Contains(haystack, "q") {
			score += 1
		}

		area := element.Width * element.Height
		if area >= 20000 {
			score += 8
		} else if area >= 12000 {
			score += 6
		} else if area >= 6000 {
			score += 3
		} else if area >= 2500 {
			score += 1
		}

		candidates = append(candidates, candidate{
			element: element,
			score:   score,
			area:    area,
		})
	}

	if len(candidates) == 0 {
		return ""
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].area > candidates[j].area
		}
		return candidates[i].score > candidates[j].score
	})
	return bestSelectorForElement(candidates[0].element)
}

func bestSelectorForElement(element pageElement) string {
	tag := strings.ToLower(strings.TrimSpace(element.Tag))
	if tag == "" {
		tag = "input"
	}

	if id := strings.TrimSpace(element.ID); id != "" {
		return "#" + cssEscaped(id)
	}

	name := strings.TrimSpace(element.Name)
	typ := strings.TrimSpace(element.Type)
	aria := strings.TrimSpace(element.AriaLabel)
	placeholder := strings.TrimSpace(element.Placeholder)

	if name != "" && placeholder != "" {
		return fmt.Sprintf(`%s[name="%s"][placeholder="%s"]`, tag, cssEscaped(name), cssEscaped(placeholder))
	}
	if name != "" && aria != "" {
		return fmt.Sprintf(`%s[name="%s"][aria-label="%s"]`, tag, cssEscaped(name), cssEscaped(aria))
	}
	if name != "" && typ != "" {
		return fmt.Sprintf(`%s[name="%s"][type="%s"]`, tag, cssEscaped(name), cssEscaped(typ))
	}
	if name != "" {
		return fmt.Sprintf(`%s[name="%s"]`, tag, cssEscaped(name))
	}
	if typ != "" {
		return fmt.Sprintf(`%s[type="%s"]`, tag, cssEscaped(typ))
	}
	if aria != "" {
		return fmt.Sprintf(`%s[aria-label="%s"]`, tag, cssEscaped(aria))
	}
	if placeholder != "" {
		return fmt.Sprintf(`%s[placeholder="%s"]`, tag, cssEscaped(placeholder))
	}
	return strings.TrimSpace(element.Selector)
}

func cssEscaped(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return escaped
}
