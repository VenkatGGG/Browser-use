package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseSearchQuery(t *testing.T) {
	t.Parallel()

	cases := []struct {
		goal string
		want string
	}{
		{goal: "search for browser use", want: "browser use"},
		{goal: "Search cats", want: "cats"},
		{goal: "find \"cheap flights to nyc\"", want: "cheap flights to nyc"},
		{goal: "open homepage", want: ""},
	}

	for _, tc := range cases {
		got := parseSearchQuery(tc.goal)
		if got != tc.want {
			t.Fatalf("parseSearchQuery(%q)=%q want=%q", tc.goal, got, tc.want)
		}
	}
}

func TestHeuristicPlannerBuildsSearchFlow(t *testing.T) {
	t.Parallel()

	planner := &heuristicPlanner{}
	snapshot := pageSnapshot{
		URL:   "https://duckduckgo.com",
		Title: "DuckDuckGo",
		Elements: []pageElement{
			{Tag: "input", Type: "search", Name: "q", Selector: "input[name=\"q\"]"},
		},
	}

	actions, err := planner.Plan(context.Background(), "search for browser automation", snapshot)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if len(actions) != 5 {
		t.Fatalf("expected 5 actions, got %d", len(actions))
	}
	if actions[2].Type != "press_enter" {
		t.Fatalf("expected action 3 type press_enter, got %s", actions[2].Type)
	}
	if actions[3].Type != "wait_for_url_contains" {
		t.Fatalf("expected action 4 type wait_for_url_contains, got %s", actions[3].Type)
	}
	if actions[3].Text != "browser automation" {
		t.Fatalf("expected action 4 text browser automation, got %q", actions[3].Text)
	}
	if actions[0].Selector != `input[name="q"][type="search"]` {
		t.Fatalf("unexpected selected input selector: %s", actions[0].Selector)
	}
}

func TestHeuristicPlannerReturnsNoActionsForUnsupportedGoal(t *testing.T) {
	t.Parallel()

	planner := &heuristicPlanner{}
	actions, err := planner.Plan(context.Background(), "open homepage and take screenshot", pageSnapshot{})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("expected no actions, got %d", len(actions))
	}
}

func TestTemplatePlannerBuildsPriceExtractionFlow(t *testing.T) {
	t.Parallel()

	planner := &templatePlanner{fallback: &heuristicPlanner{}}
	snapshot := pageSnapshot{
		URL:   "https://www.amazon.com",
		Title: "Amazon",
		Elements: []pageElement{
			{Tag: "input", Type: "search", Name: "field-keywords", Selector: "input#twotabsearchtextbox", Width: 560, Height: 40},
		},
	}

	actions, err := planner.Plan(context.Background(), `search for "think and grow rich" on amazon and give me the price`, snapshot)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if len(actions) < 5 {
		t.Fatalf("expected at least 5 actions, got %d", len(actions))
	}
	last := actions[len(actions)-1]
	if last.Type != "extract_text" {
		t.Fatalf("expected final action extract_text, got %s", last.Type)
	}
	if last.Selector == "" || last.TimeoutMS <= 0 {
		t.Fatalf("expected extract_text selector and timeout, got selector=%q timeout=%d", last.Selector, last.TimeoutMS)
	}
	if !strings.Contains(last.Selector, "a-price") {
		t.Fatalf("expected amazon price selector, got %q", last.Selector)
	}
}

func TestTemplatePlannerFallsBackToHeuristicForSimpleSearch(t *testing.T) {
	t.Parallel()

	planner := &templatePlanner{fallback: &heuristicPlanner{}}
	snapshot := pageSnapshot{
		URL:   "https://duckduckgo.com",
		Title: "DuckDuckGo",
		Elements: []pageElement{
			{Tag: "input", Type: "search", Name: "q", Selector: "input[name=\"q\"]", Width: 500, Height: 40},
		},
	}

	actions, err := planner.Plan(context.Background(), "search for browser use", snapshot)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if len(actions) == 0 {
		t.Fatalf("expected non-empty fallback actions")
	}
	if actions[len(actions)-1].Type == "extract_text" {
		t.Fatalf("did not expect extract_text for simple search flow")
	}
}

func TestBestSelectorForElementPriority(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   pageElement
		want string
	}{
		{
			name: "id first",
			in:   pageElement{Tag: "input", ID: "main-search", Name: "q", Type: "search"},
			want: "#main-search",
		},
		{
			name: "name and type",
			in:   pageElement{Tag: "input", Name: "q", Type: "search"},
			want: `input[name="q"][type="search"]`,
		},
		{
			name: "name and placeholder",
			in:   pageElement{Tag: "input", Name: "q", Placeholder: "Search privately"},
			want: `input[name="q"][placeholder="Search privately"]`,
		},
		{
			name: "name only",
			in:   pageElement{Tag: "input", Name: "query"},
			want: `input[name="query"]`,
		},
		{
			name: "type fallback",
			in:   pageElement{Tag: "input", Type: "search"},
			want: `input[type="search"]`,
		},
		{
			name: "fallback selector",
			in:   pageElement{Tag: "input", Selector: "div:nth-of-type(1) > input:nth-of-type(1)"},
			want: "div:nth-of-type(1) > input:nth-of-type(1)",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := bestSelectorForElement(tc.in)
			if got != tc.want {
				t.Fatalf("bestSelectorForElement mismatch: got=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestBuildPlannerStatePacketCompactsAndLimitsElements(t *testing.T) {
	t.Parallel()

	snapshot := pageSnapshot{
		URL:            "https://example.com",
		Title:          "Example",
		ViewportWidth:  1280,
		ViewportHeight: 720,
		ScrollX:        10,
		ScrollY:        20,
		Elements: []pageElement{
			{
				StableID:    "el_a",
				Tag:         "input",
				Type:        "search",
				Name:        "q",
				Placeholder: "Search",
				Selector:    "input[name=\"q\"]",
				X:           100,
				Y:           200,
				Width:       400,
				Height:      40,
			},
			{
				StableID: "el_b",
				Tag:      "button",
				Text:     "Search",
				Selector: "button[type=\"submit\"]",
				X:        520,
				Y:        200,
				Width:    100,
				Height:   40,
			},
		},
	}

	packet := buildPlannerStatePacket("search for browser use", snapshot, 1)
	if len(packet.Elements) != 1 {
		t.Fatalf("expected 1 compact element, got %d", len(packet.Elements))
	}
	if packet.Elements[0].ID != "el_a" {
		t.Fatalf("unexpected compact element id: %s", packet.Elements[0].ID)
	}
	if packet.Viewport.Width != 1280 || packet.Viewport.Height != 720 {
		t.Fatalf("unexpected viewport dimensions: %+v", packet.Viewport)
	}
	if packet.Viewport.ScrollX != 10 || packet.Viewport.ScrollY != 20 {
		t.Fatalf("unexpected viewport scroll offsets: %+v", packet.Viewport)
	}
}

func TestEndpointPlannerUsesEndpointActions(t *testing.T) {
	t.Parallel()

	var captured endpointPlanRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"actions": []map[string]any{
				{"type": "wait", "delay_ms": 600},
			},
		})
	}))
	defer server.Close()

	planner := &endpointPlanner{
		endpointURL: server.URL,
		timeout:     2 * time.Second,
		maxElements: 10,
		client:      server.Client(),
		fallback:    &heuristicPlanner{},
	}

	snapshot := pageSnapshot{
		URL:   "https://example.com",
		Title: "Example",
		Elements: []pageElement{
			{Tag: "button", Text: "Continue", Selector: "button.primary", StableID: "el_1", Width: 90, Height: 32},
		},
	}
	actions, err := planner.Plan(context.Background(), "continue", snapshot)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if len(actions) != 1 || actions[0].Type != "wait" {
		t.Fatalf("unexpected endpoint actions: %+v", actions)
	}
	if captured.Goal != "continue" {
		t.Fatalf("expected goal in endpoint payload, got %q", captured.Goal)
	}
	if len(captured.State.Elements) != 1 {
		t.Fatalf("expected compact state elements in payload, got %d", len(captured.State.Elements))
	}
}

func TestEndpointPlannerFallsBackToHeuristicOnEndpointFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	planner := &endpointPlanner{
		endpointURL: server.URL,
		timeout:     2 * time.Second,
		maxElements: 10,
		client:      server.Client(),
		fallback:    &heuristicPlanner{},
	}

	snapshot := pageSnapshot{
		URL:   "https://duckduckgo.com",
		Title: "DuckDuckGo",
		Elements: []pageElement{
			{Tag: "input", Type: "search", Name: "q", Selector: "input[name=\"q\"]", Width: 480, Height: 42},
		},
	}

	actions, err := planner.Plan(context.Background(), "search for browser use", snapshot)
	if err != nil {
		t.Fatalf("expected fallback planner to succeed, got error: %v", err)
	}
	if len(actions) == 0 {
		t.Fatalf("expected fallback heuristic actions, got none")
	}
	if actions[0].Type != "wait_for" {
		t.Fatalf("expected first fallback action wait_for, got %s", actions[0].Type)
	}
}

func TestNewActionPlannerSelectsExpectedMode(t *testing.T) {
	t.Parallel()

	if planner := newActionPlanner(plannerConfig{Mode: "off"}); planner != nil {
		t.Fatalf("expected nil planner for off mode")
	}
	if planner := newActionPlanner(plannerConfig{}); planner == nil || planner.Name() != "template" {
		t.Fatalf("expected template planner by default")
	}
	if planner := newActionPlanner(plannerConfig{Mode: "template"}); planner == nil || planner.Name() != "template" {
		t.Fatalf("expected template planner for template mode")
	}
	if planner := newActionPlanner(plannerConfig{Mode: "endpoint"}); planner == nil || planner.Name() != "template" {
		t.Fatalf("expected template fallback when endpoint url is missing")
	}
	if planner := newActionPlanner(plannerConfig{Mode: "endpoint", EndpointURL: "http://planner.local"}); planner == nil || planner.Name() != "endpoint" {
		t.Fatalf("expected endpoint planner when endpoint url is provided")
	}
}
