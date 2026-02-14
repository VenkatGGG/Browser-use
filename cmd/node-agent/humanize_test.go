package main

import (
	"math/rand"
	"testing"
)

func TestHumanizeProfileForMode(t *testing.T) {
	if _, ok := humanizeProfileForMode("off"); ok {
		t.Fatalf("expected off mode to disable humanizer")
	}

	profile, ok := humanizeProfileForMode("balanced")
	if !ok {
		t.Fatalf("expected balanced mode to be enabled")
	}
	if profile.MouseMinSteps <= 0 || profile.MouseMaxSteps < profile.MouseMinSteps {
		t.Fatalf("invalid balanced mouse step bounds: %+v", profile)
	}
	if profile.TypeKeyMinMS <= 0 || profile.TypeKeyMaxMS < profile.TypeKeyMinMS {
		t.Fatalf("invalid balanced typing bounds: %+v", profile)
	}
}

func TestBuildMousePathIncludesEndpoints(t *testing.T) {
	h := newHumanizer("balanced", 42)
	if h == nil {
		t.Fatalf("expected humanizer to be created")
	}

	start := point2D{X: 24, Y: 34}
	end := point2D{X: 620, Y: 390}
	viewport := viewportSize{Width: 1280, Height: 720}
	path := h.buildMousePath(start, end, viewport, false)
	if len(path) < 3 {
		t.Fatalf("expected at least 3 path points, got %d", len(path))
	}
	if path[0] != start {
		t.Fatalf("expected first point %+v, got %+v", start, path[0])
	}
	if path[len(path)-1] != end {
		t.Fatalf("expected last point %+v, got %+v", end, path[len(path)-1])
	}
	for index, p := range path {
		if p.X < 1 || p.X > viewport.Width-1 || p.Y < 1 || p.Y > viewport.Height-1 {
			t.Fatalf("point %d out of viewport bounds: %+v", index, p)
		}
	}
}

func TestPlanScrollBurstDeltasConservesTotal(t *testing.T) {
	rng := rand.New(rand.NewSource(77))
	deltas := planScrollBurstDeltas(7, 910, rng)
	if len(deltas) != 7 {
		t.Fatalf("expected 7 deltas, got %d", len(deltas))
	}
	total := 0
	for i, d := range deltas {
		if d <= 0 {
			t.Fatalf("delta %d must be >0, got %d", i, d)
		}
		total += d
	}
	if total != 910 {
		t.Fatalf("expected delta sum 910, got %d (deltas=%v)", total, deltas)
	}
}

func TestNearbyTypoRune(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	typo, ok := nearbyTypoRune('A', rng)
	if !ok {
		t.Fatalf("expected typo candidate for letter")
	}
	if typo == 'A' {
		t.Fatalf("expected typo rune to differ from original")
	}
	if typo < 'A' || typo > 'Z' {
		t.Fatalf("expected uppercase typo rune, got %q", typo)
	}
}
