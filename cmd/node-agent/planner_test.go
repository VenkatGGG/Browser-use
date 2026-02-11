package main

import (
	"context"
	"testing"
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

func TestHeuristicPlannerUsesRelativeSubmitButton(t *testing.T) {
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
	if len(actions) != 4 {
		t.Fatalf("expected 4 actions, got %d", len(actions))
	}
	if actions[2].Type != "press_enter" {
		t.Fatalf("expected action 3 type press_enter, got %s", actions[2].Type)
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
