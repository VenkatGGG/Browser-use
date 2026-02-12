package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type plannerEvalCase struct {
	Name        string       `json:"name"`
	PlannerMode string       `json:"planner_mode"`
	Goal        string       `json:"goal"`
	Snapshot    pageSnapshot `json:"snapshot"`
	Expect      struct {
		MinActions          int      `json:"min_actions"`
		MaxActions          int      `json:"max_actions"`
		ContainsActionTypes []string `json:"contains_action_types"`
		ExcludedActionTypes []string `json:"excluded_action_types"`
		FirstActionType     string   `json:"first_action_type"`
		LastActionType      string   `json:"last_action_type"`
		SelectorSubstrings  []string `json:"selector_substrings"`
	} `json:"expect"`
}

func TestPlannerEvalFixtures(t *testing.T) {
	t.Parallel()

	cases := loadPlannerEvalCases(t)
	if len(cases) == 0 {
		t.Fatalf("expected planner eval fixtures, got 0")
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			validatePlannerEvalCase(t, tc)
		})
	}
}

func loadPlannerEvalCases(t *testing.T) []plannerEvalCase {
	t.Helper()

	raw, err := os.ReadFile("testdata/planner_eval_cases.json")
	if err != nil {
		t.Fatalf("read planner eval fixtures: %v", err)
	}

	var cases []plannerEvalCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("decode planner eval fixtures: %v", err)
	}
	return cases
}

func validatePlannerEvalCase(t *testing.T, tc plannerEvalCase) {
	t.Helper()

	mode := strings.TrimSpace(tc.PlannerMode)
	if mode == "" {
		t.Fatalf("planner_mode is required")
	}

	planner := newActionPlanner(plannerConfig{
		Mode: mode,
	})
	if planner == nil {
		t.Fatalf("planner mode %q returned nil planner", mode)
	}

	actions, err := planner.Plan(context.Background(), tc.Goal, tc.Snapshot)
	if err != nil {
		t.Fatalf("planner=%s returned error: %v", planner.Name(), err)
	}

	if got, min := len(actions), tc.Expect.MinActions; got < min {
		t.Fatalf("planner=%s produced too few actions: got=%d min=%d actions=%s", planner.Name(), got, min, summarizeActions(actions))
	}
	if max := tc.Expect.MaxActions; max > 0 && len(actions) > max {
		t.Fatalf("planner=%s produced too many actions: got=%d max=%d actions=%s", planner.Name(), len(actions), max, summarizeActions(actions))
	}

	if first := strings.TrimSpace(tc.Expect.FirstActionType); first != "" {
		if len(actions) == 0 || strings.ToLower(strings.TrimSpace(actions[0].Type)) != strings.ToLower(first) {
			t.Fatalf("expected first action type %q, got=%s", first, summarizeActions(actions))
		}
	}
	if last := strings.TrimSpace(tc.Expect.LastActionType); last != "" {
		if len(actions) == 0 || strings.ToLower(strings.TrimSpace(actions[len(actions)-1].Type)) != strings.ToLower(last) {
			t.Fatalf("expected last action type %q, got=%s", last, summarizeActions(actions))
		}
	}

	typeCounts := map[string]int{}
	for _, action := range actions {
		typeCounts[strings.ToLower(strings.TrimSpace(action.Type))]++
	}

	for _, requiredType := range tc.Expect.ContainsActionTypes {
		key := strings.ToLower(strings.TrimSpace(requiredType))
		if key == "" {
			continue
		}
		if typeCounts[key] == 0 {
			t.Fatalf("expected action type %q missing; actions=%s", key, summarizeActions(actions))
		}
	}
	for _, excludedType := range tc.Expect.ExcludedActionTypes {
		key := strings.ToLower(strings.TrimSpace(excludedType))
		if key == "" {
			continue
		}
		if typeCounts[key] > 0 {
			t.Fatalf("unexpected excluded action type %q present; actions=%s", key, summarizeActions(actions))
		}
	}

	for _, snippet := range tc.Expect.SelectorSubstrings {
		needle := strings.ToLower(strings.TrimSpace(snippet))
		if needle == "" {
			continue
		}
		if !anySelectorContains(actions, needle) {
			t.Fatalf("expected selector substring %q not found; actions=%s", snippet, summarizeActions(actions))
		}
	}
}

func anySelectorContains(actions []executeAction, needle string) bool {
	for _, action := range actions {
		if strings.Contains(strings.ToLower(strings.TrimSpace(action.Selector)), needle) {
			return true
		}
	}
	return false
}

func TestPlannerEvalFixturesAreNamedUniquely(t *testing.T) {
	t.Parallel()

	cases := loadPlannerEvalCases(t)
	seen := map[string]struct{}{}
	for _, tc := range cases {
		name := strings.TrimSpace(tc.Name)
		if name == "" {
			t.Fatalf("fixture has empty name")
		}
		if _, exists := seen[name]; exists {
			t.Fatalf("duplicate fixture name: %s", name)
		}
		seen[name] = struct{}{}
	}
}
