package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"time"
	"unicode"

	"github.com/VenkatGGG/Browser-use/internal/cdp"
)

type humanizer struct {
	profile   humanizeProfile
	rng       *rand.Rand
	cursor    point2D
	hasCursor bool
}

type humanizeProfile struct {
	MouseMoveStepMinMS   int
	MouseMoveStepMaxMS   int
	MouseMinSteps        int
	MouseMaxSteps        int
	MouseJitterPX        float64
	MouseOvershootChance float64
	MouseOvershootMinPX  float64
	MouseOvershootMaxPX  float64

	ClickDwellMinMS int
	ClickDwellMaxMS int
	ClickHoldMinMS  int
	ClickHoldMaxMS  int

	TypeKeyMinMS   int
	TypeKeyMaxMS   int
	TypePauseMinMS int
	TypePauseMaxMS int
	TypoChance     float64
	TypoFixMinMS   int
	TypoFixMaxMS   int

	ScrollBurstEventsMin   int
	ScrollBurstEventsMax   int
	ScrollDeltaMinPX       int
	ScrollDeltaMaxPX       int
	ScrollEventDelayMinMS  int
	ScrollEventDelayMaxMS  int
	ScrollBurstPauseMinMS  int
	ScrollBurstPauseMaxMS  int
	ScrollCorrectionChance float64
	ScrollCorrectionMinPX  int
	ScrollCorrectionMaxPX  int
}

type point2D struct {
	X float64
	Y float64
}

type viewportSize struct {
	Width  float64
	Height float64
}

type elementGeometry struct {
	OK             bool    `json:"ok"`
	Error          string  `json:"error"`
	Left           float64 `json:"left"`
	Top            float64 `json:"top"`
	Width          float64 `json:"width"`
	Height         float64 `json:"height"`
	ViewportWidth  float64 `json:"viewport_width"`
	ViewportHeight float64 `json:"viewport_height"`
}

func newHumanizer(mode string, seed int64) *humanizer {
	profile, enabled := humanizeProfileForMode(mode)
	if !enabled {
		return nil
	}

	if seed == 0 {
		seed = time.Now().UnixNano()
	}

	return &humanizer{
		profile: profile,
		rng:     rand.New(rand.NewSource(seed)),
	}
}

func humanizeProfileForMode(mode string) (humanizeProfile, bool) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "off", "disabled", "none", "false", "0":
		return humanizeProfile{}, false
	case "on", "true", "enabled", "balanced":
		return humanizeProfile{
			MouseMoveStepMinMS:   6,
			MouseMoveStepMaxMS:   16,
			MouseMinSteps:        12,
			MouseMaxSteps:        58,
			MouseJitterPX:        1.4,
			MouseOvershootChance: 0.14,
			MouseOvershootMinPX:  6,
			MouseOvershootMaxPX:  20,

			ClickDwellMinMS: 45,
			ClickDwellMaxMS: 140,
			ClickHoldMinMS:  20,
			ClickHoldMaxMS:  82,

			TypeKeyMinMS:   24,
			TypeKeyMaxMS:   130,
			TypePauseMinMS: 70,
			TypePauseMaxMS: 220,
			TypoChance:     0.015,
			TypoFixMinMS:   35,
			TypoFixMaxMS:   110,

			ScrollBurstEventsMin:   2,
			ScrollBurstEventsMax:   6,
			ScrollDeltaMinPX:       32,
			ScrollDeltaMaxPX:       180,
			ScrollEventDelayMinMS:  11,
			ScrollEventDelayMaxMS:  34,
			ScrollBurstPauseMinMS:  120,
			ScrollBurstPauseMaxMS:  460,
			ScrollCorrectionChance: 0.12,
			ScrollCorrectionMinPX:  10,
			ScrollCorrectionMaxPX:  80,
		}, true
	case "aggressive":
		return humanizeProfile{
			MouseMoveStepMinMS:   4,
			MouseMoveStepMaxMS:   12,
			MouseMinSteps:        18,
			MouseMaxSteps:        76,
			MouseJitterPX:        2.1,
			MouseOvershootChance: 0.30,
			MouseOvershootMinPX:  8,
			MouseOvershootMaxPX:  28,

			ClickDwellMinMS: 58,
			ClickDwellMaxMS: 220,
			ClickHoldMinMS:  28,
			ClickHoldMaxMS:  120,

			TypeKeyMinMS:   35,
			TypeKeyMaxMS:   190,
			TypePauseMinMS: 110,
			TypePauseMaxMS: 360,
			TypoChance:     0.030,
			TypoFixMinMS:   50,
			TypoFixMaxMS:   170,

			ScrollBurstEventsMin:   3,
			ScrollBurstEventsMax:   8,
			ScrollDeltaMinPX:       28,
			ScrollDeltaMaxPX:       210,
			ScrollEventDelayMinMS:  9,
			ScrollEventDelayMaxMS:  42,
			ScrollBurstPauseMinMS:  170,
			ScrollBurstPauseMaxMS:  650,
			ScrollCorrectionChance: 0.20,
			ScrollCorrectionMinPX:  14,
			ScrollCorrectionMaxPX:  120,
		}, true
	default:
		return humanizeProfile{}, false
	}
}

func (h *humanizer) Click(ctx context.Context, client *cdp.Client, selector string) error {
	trimmedSelector := strings.TrimSpace(selector)
	if trimmedSelector == "" {
		return errors.New("selector is required")
	}

	geometry, err := h.resolveElementGeometry(ctx, client, trimmedSelector)
	if err != nil {
		return err
	}
	viewport := normalizeViewportSize(viewportSize{
		Width:  geometry.ViewportWidth,
		Height: geometry.ViewportHeight,
	})
	target := h.pickTargetPoint(geometry, viewport)
	start := h.currentOrSeedCursor(target, viewport)

	path := h.buildMousePath(start, target, viewport, true)
	for index, p := range path {
		if err := dispatchMouseEvent(ctx, client, "mouseMoved", p, "none", 0, 0, 0); err != nil {
			return err
		}
		if index < len(path)-1 {
			if err := h.sleepRandom(ctx, h.profile.MouseMoveStepMinMS, h.profile.MouseMoveStepMaxMS); err != nil {
				return err
			}
		}
	}

	if err := h.sleepRandom(ctx, h.profile.ClickDwellMinMS, h.profile.ClickDwellMaxMS); err != nil {
		return err
	}
	if err := dispatchMouseEvent(ctx, client, "mousePressed", target, "left", 1, 0, 0); err != nil {
		return err
	}
	if err := h.sleepRandom(ctx, h.profile.ClickHoldMinMS, h.profile.ClickHoldMaxMS); err != nil {
		return err
	}
	if err := dispatchMouseEvent(ctx, client, "mouseReleased", target, "left", 1, 0, 0); err != nil {
		return err
	}

	h.cursor = target
	h.hasCursor = true
	return nil
}

func (h *humanizer) Type(ctx context.Context, client *cdp.Client, selector, text string) error {
	trimmedSelector := strings.TrimSpace(selector)
	if trimmedSelector == "" {
		return errors.New("selector is required")
	}

	if err := h.Click(ctx, client, trimmedSelector); err != nil {
		return err
	}
	if err := h.focusAndClearForType(ctx, client, trimmedSelector); err != nil {
		return err
	}

	typedTypo := false
	for _, r := range []rune(text) {
		if !typedTypo && h.shouldInjectTypo(r) {
			if typo, ok := nearbyTypoRune(r, h.rng); ok {
				if err := insertText(ctx, client, string(typo)); err != nil {
					return err
				}
				if err := sleepWithContext(ctx, h.keyDelayForRune(typo)); err != nil {
					return err
				}
				if err := dispatchBackspace(ctx, client); err != nil {
					return err
				}
				if err := h.sleepRandom(ctx, h.profile.TypoFixMinMS, h.profile.TypoFixMaxMS); err != nil {
					return err
				}
				typedTypo = true
			}
		}

		if err := insertText(ctx, client, string(r)); err != nil {
			return err
		}
		if err := sleepWithContext(ctx, h.keyDelayForRune(r)); err != nil {
			return err
		}
	}
	return nil
}

func (h *humanizer) Scroll(ctx context.Context, client *cdp.Client, direction string, pixels int) error {
	dir := strings.ToLower(strings.TrimSpace(direction))
	if dir == "" {
		dir = "down"
	}
	if pixels <= 0 {
		pixels = 600
	}
	if pixels > 3000 {
		pixels = 3000
	}

	if dir == "top" || dir == "bottom" {
		return client.Scroll(ctx, dir, pixels)
	}

	axisY := true
	sign := 1
	switch dir {
	case "up":
		sign = -1
	case "down":
		sign = 1
	case "left":
		axisY = false
		sign = -1
	case "right":
		axisY = false
		sign = 1
	default:
		axisY = true
		sign = 1
	}

	wheelPoint, viewport, err := h.currentWheelPoint(ctx, client)
	if err != nil {
		return err
	}

	remaining := absInt(pixels)
	for remaining > 0 {
		eventCount := h.randomInt(h.profile.ScrollBurstEventsMin, h.profile.ScrollBurstEventsMax)
		if eventCount <= 0 {
			eventCount = 1
		}

		minBurst := eventCount * maxIntValue(16, h.profile.ScrollDeltaMinPX/2)
		maxBurst := eventCount * maxIntValue(minBurst, h.profile.ScrollDeltaMaxPX)
		if maxBurst < minBurst {
			maxBurst = minBurst
		}

		burstTarget := h.randomInt(minBurst, maxBurst)
		if burstTarget > remaining {
			burstTarget = remaining
		}
		if burstTarget <= 0 {
			burstTarget = remaining
		}

		deltas := planScrollBurstDeltas(eventCount, burstTarget, h.rng)
		for _, delta := range deltas {
			signedDelta := sign * maxIntValue(1, delta)
			deltaX := 0.0
			deltaY := 0.0
			if axisY {
				deltaY = float64(signedDelta)
			} else {
				deltaX = float64(signedDelta)
			}
			if err := dispatchMouseEvent(ctx, client, "mouseWheel", wheelPoint, "none", 0, deltaX, deltaY); err != nil {
				return err
			}
			if err := h.sleepRandom(ctx, h.profile.ScrollEventDelayMinMS, h.profile.ScrollEventDelayMaxMS); err != nil {
				return err
			}
			remaining -= maxIntValue(1, delta)
			if remaining <= 0 {
				break
			}
		}

		if remaining > 0 {
			if err := h.sleepRandom(ctx, h.profile.ScrollBurstPauseMinMS, h.profile.ScrollBurstPauseMaxMS); err != nil {
				return err
			}
		}
	}

	if h.chance(h.profile.ScrollCorrectionChance) {
		correction := h.randomInt(h.profile.ScrollCorrectionMinPX, h.profile.ScrollCorrectionMaxPX)
		if correction > 0 {
			signed := -sign * correction
			deltaX := 0.0
			deltaY := 0.0
			if axisY {
				deltaY = float64(signed)
			} else {
				deltaX = float64(signed)
			}
			if err := dispatchMouseEvent(ctx, client, "mouseWheel", wheelPoint, "none", 0, deltaX, deltaY); err != nil {
				return err
			}
		}
	}

	h.cursor = clampPoint(wheelPoint, viewport)
	h.hasCursor = true
	return nil
}

func (h *humanizer) resolveElementGeometry(ctx context.Context, client *cdp.Client, selector string) (elementGeometry, error) {
	expression := fmt.Sprintf(`(() => {
		const visible = (node) => {
			const style = window.getComputedStyle(node);
			if (!style || style.display === "none" || style.visibility === "hidden") return false;
			const rect = node.getBoundingClientRect();
			return rect.width > 1 && rect.height > 1;
		};
		const el = Array.from(document.querySelectorAll(%q)).find(visible);
		if (!el) return { ok: false, error: "not_found" };
		el.scrollIntoView({ block: "center", inline: "center" });
		if (typeof el.focus === "function") el.focus();
		const rect = el.getBoundingClientRect();
		return {
			ok: true,
			left: Number(rect.left || 0),
			top: Number(rect.top || 0),
			width: Number(rect.width || 0),
			height: Number(rect.height || 0),
			viewport_width: Number(window.innerWidth || 0),
			viewport_height: Number(window.innerHeight || 0)
		};
	})()`, selector)

	value, err := client.EvaluateAny(ctx, expression)
	if err != nil {
		return elementGeometry{}, err
	}

	payload, err := json.Marshal(value)
	if err != nil {
		return elementGeometry{}, err
	}

	var geometry elementGeometry
	if err := json.Unmarshal(payload, &geometry); err != nil {
		return elementGeometry{}, err
	}
	if !geometry.OK {
		reason := strings.TrimSpace(geometry.Error)
		if reason == "" {
			reason = "not_found"
		}
		return elementGeometry{}, fmt.Errorf("element lookup failed: %s", reason)
	}
	if geometry.Width <= 1 || geometry.Height <= 1 {
		return elementGeometry{}, errors.New("element has invalid geometry")
	}
	return geometry, nil
}

func (h *humanizer) focusAndClearForType(ctx context.Context, client *cdp.Client, selector string) error {
	expression := fmt.Sprintf(`(() => {
		const visible = (node) => {
			const style = window.getComputedStyle(node);
			if (!style || style.display === "none" || style.visibility === "hidden") return false;
			const rect = node.getBoundingClientRect();
			return rect.width > 1 && rect.height > 1;
		};
		const el = Array.from(document.querySelectorAll(%q)).find(visible);
		if (!el) return "not_found";
		el.scrollIntoView({ block: "center", inline: "center" });
		el.focus();
		if ("value" in el) {
			el.value = "";
			el.dispatchEvent(new Event("input", { bubbles: true }));
		}
		return "ok";
	})()`, selector)

	result, err := client.EvaluateString(ctx, expression)
	if err != nil {
		return err
	}
	if strings.TrimSpace(result) != "ok" {
		return fmt.Errorf("focus failed: %s", strings.TrimSpace(result))
	}
	return nil
}

func (h *humanizer) currentWheelPoint(ctx context.Context, client *cdp.Client) (point2D, viewportSize, error) {
	viewport, err := resolveViewport(ctx, client)
	if err != nil {
		return point2D{}, viewportSize{}, err
	}
	viewport = normalizeViewportSize(viewport)

	if h.hasCursor {
		return clampPoint(h.cursor, viewport), viewport, nil
	}

	seed := point2D{
		X: viewport.Width*0.50 + h.randomFloat(-viewport.Width*0.08, viewport.Width*0.08),
		Y: viewport.Height*0.58 + h.randomFloat(-viewport.Height*0.11, viewport.Height*0.11),
	}
	seed = clampPoint(seed, viewport)
	h.cursor = seed
	h.hasCursor = true
	return seed, viewport, nil
}

func resolveViewport(ctx context.Context, client *cdp.Client) (viewportSize, error) {
	value, err := client.EvaluateAny(ctx, `(() => ({
		width: Number(window.innerWidth || 0),
		height: Number(window.innerHeight || 0)
	}))()`)
	if err != nil {
		return viewportSize{}, err
	}

	payload, err := json.Marshal(value)
	if err != nil {
		return viewportSize{}, err
	}

	var decoded struct {
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return viewportSize{}, err
	}

	return viewportSize{Width: decoded.Width, Height: decoded.Height}, nil
}

func (h *humanizer) pickTargetPoint(geometry elementGeometry, viewport viewportSize) point2D {
	minPaddingX := clampFloat(geometry.Width*0.18, 2, 12)
	minPaddingY := clampFloat(geometry.Height*0.18, 2, 12)

	xMin := geometry.Left + minPaddingX
	xMax := geometry.Left + geometry.Width - minPaddingX
	yMin := geometry.Top + minPaddingY
	yMax := geometry.Top + geometry.Height - minPaddingY

	if xMax <= xMin {
		xMin = geometry.Left
		xMax = geometry.Left + geometry.Width
	}
	if yMax <= yMin {
		yMin = geometry.Top
		yMax = geometry.Top + geometry.Height
	}

	target := point2D{
		X: xMin + (xMax-xMin)*h.randomFloat(0, 1),
		Y: yMin + (yMax-yMin)*h.randomFloat(0, 1),
	}
	return clampPoint(target, viewport)
}

func (h *humanizer) currentOrSeedCursor(target point2D, viewport viewportSize) point2D {
	if h.hasCursor {
		return clampPoint(h.cursor, viewport)
	}

	seed := point2D{
		X: target.X + signedRandom(h.rng, 34, 180),
		Y: target.Y + signedRandom(h.rng, 28, 140),
	}
	seed = clampPoint(seed, viewport)
	h.cursor = seed
	h.hasCursor = true
	return seed
}

func (h *humanizer) buildMousePath(start, end point2D, viewport viewportSize, allowOvershoot bool) []point2D {
	start = clampPoint(start, viewport)
	end = clampPoint(end, viewport)
	if allowOvershoot && h.chance(h.profile.MouseOvershootChance) && distance(start, end) > 80 {
		dx := end.X - start.X
		dy := end.Y - start.Y
		length := math.Hypot(dx, dy)
		if length > 0 {
			overshootDistance := h.randomFloat(h.profile.MouseOvershootMinPX, h.profile.MouseOvershootMaxPX)
			overshoot := point2D{
				X: end.X + (dx/length)*overshootDistance,
				Y: end.Y + (dy/length)*overshootDistance,
			}
			overshoot = clampPoint(overshoot, viewport)
			first := h.buildBezierSegment(start, overshoot, viewport)
			second := h.buildBezierSegment(overshoot, end, viewport)
			if len(first) > 1 && len(second) > 0 {
				return append(first[:len(first)-1], second...)
			}
		}
	}
	return h.buildBezierSegment(start, end, viewport)
}

func (h *humanizer) buildBezierSegment(start, end point2D, viewport viewportSize) []point2D {
	d := distance(start, end)
	steps := int(math.Round(d/9.5)) + h.randomInt(-2, 3)
	steps = clampIntValue(steps, h.profile.MouseMinSteps, h.profile.MouseMaxSteps)
	if steps < 2 {
		steps = 2
	}

	dx := end.X - start.X
	dy := end.Y - start.Y
	length := math.Hypot(dx, dy)
	if length < 0.001 {
		return []point2D{start, end}
	}

	perpX := -dy / length
	perpY := dx / length
	curve := length * h.randomFloat(0.07, 0.19)
	if h.chance(0.5) {
		curve = -curve
	}

	c1 := point2D{
		X: start.X + dx*0.33 + perpX*curve,
		Y: start.Y + dy*0.33 + perpY*curve,
	}
	c2 := point2D{
		X: start.X + dx*0.66 - perpX*curve*h.randomFloat(0.55, 1.05),
		Y: start.Y + dy*0.66 - perpY*curve*h.randomFloat(0.55, 1.05),
	}

	path := make([]point2D, 0, steps+1)
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		p := cubicBezierPoint(start, c1, c2, end, t)

		if i > 0 && i < steps {
			jitter := h.profile.MouseJitterPX * 0.20
			if i >= steps-3 {
				jitter = h.profile.MouseJitterPX
			}
			p.X += h.randomFloat(-jitter, jitter)
			p.Y += h.randomFloat(-jitter, jitter)
		}

		path = append(path, clampPoint(p, viewport))
	}

	if len(path) > 0 {
		path[0] = start
		path[len(path)-1] = end
	}
	return path
}

func (h *humanizer) keyDelayForRune(r rune) time.Duration {
	delayMS := float64(h.randomInt(h.profile.TypeKeyMinMS, h.profile.TypeKeyMaxMS))
	if unicode.IsUpper(r) {
		delayMS += h.randomFloat(8, 26)
	}
	if unicode.IsSpace(r) || strings.ContainsRune(".,;:!?)]}\"'", r) {
		delayMS += float64(h.randomInt(h.profile.TypePauseMinMS, h.profile.TypePauseMaxMS))
	}
	delayMS = clampFloat(delayMS, 8, 900)
	return time.Duration(delayMS) * time.Millisecond
}

func (h *humanizer) shouldInjectTypo(r rune) bool {
	if h.profile.TypoChance <= 0 {
		return false
	}
	if !unicode.IsLetter(r) {
		return false
	}
	return h.chance(h.profile.TypoChance)
}

func (h *humanizer) sleepRandom(ctx context.Context, minMS, maxMS int) error {
	if minMS <= 0 && maxMS <= 0 {
		return nil
	}
	delay := h.randomInt(minMS, maxMS)
	if delay <= 0 {
		return nil
	}
	return sleepWithContext(ctx, time.Duration(delay)*time.Millisecond)
}

func (h *humanizer) randomInt(min, max int) int {
	if max < min {
		min, max = max, min
	}
	if min == max {
		return min
	}
	return min + h.rng.Intn(max-min+1)
}

func (h *humanizer) randomFloat(min, max float64) float64 {
	if max < min {
		min, max = max, min
	}
	if min == max {
		return min
	}
	return min + h.rng.Float64()*(max-min)
}

func (h *humanizer) chance(probability float64) bool {
	if probability <= 0 {
		return false
	}
	if probability >= 1 {
		return true
	}
	return h.rng.Float64() < probability
}

func planScrollBurstDeltas(eventCount, total int, rng *rand.Rand) []int {
	if total <= 0 {
		return nil
	}
	if eventCount <= 1 {
		return []int{total}
	}
	if eventCount > 32 {
		eventCount = 32
	}

	weights := make([]float64, eventCount)
	weightSum := 0.0
	for i := 0; i < eventCount; i++ {
		t := float64(i) / float64(eventCount-1)
		triangular := 1.0 - math.Abs((2*t)-1.0)
		w := 0.45 + (0.95 * triangular)
		if rng != nil {
			w *= 0.85 + rng.Float64()*0.30
		}
		if w < 0.1 {
			w = 0.1
		}
		weights[i] = w
		weightSum += w
	}

	deltas := make([]int, eventCount)
	assigned := 0
	for i, w := range weights {
		delta := int(math.Round(float64(total) * (w / weightSum)))
		if delta < 1 {
			delta = 1
		}
		deltas[i] = delta
		assigned += delta
	}

	diff := total - assigned
	step := 1
	if diff < 0 {
		diff = -diff
		step = -1
	}
	for i := 0; i < diff; i++ {
		index := i % len(deltas)
		if step < 0 && deltas[index] <= 1 {
			continue
		}
		deltas[index] += step
	}

	return deltas
}

func dispatchMouseEvent(ctx context.Context, client *cdp.Client, eventType string, p point2D, button string, clickCount int, deltaX, deltaY float64) error {
	trimmedType := strings.TrimSpace(eventType)
	if trimmedType == "" {
		return errors.New("mouse event type is required")
	}
	if strings.TrimSpace(button) == "" {
		button = "none"
	}

	payload := map[string]any{
		"type":   trimmedType,
		"x":      roundToOneDecimal(p.X),
		"y":      roundToOneDecimal(p.Y),
		"button": button,
	}
	if clickCount > 0 {
		payload["clickCount"] = clickCount
	}
	if deltaX != 0 {
		payload["deltaX"] = roundToOneDecimal(deltaX)
	}
	if deltaY != 0 {
		payload["deltaY"] = roundToOneDecimal(deltaY)
	}

	return client.Call(ctx, "Input.dispatchMouseEvent", payload, nil)
}

func insertText(ctx context.Context, client *cdp.Client, text string) error {
	if text == "" {
		return nil
	}
	return client.Call(ctx, "Input.insertText", map[string]any{"text": text}, nil)
}

func dispatchBackspace(ctx context.Context, client *cdp.Client) error {
	events := []string{"keyDown", "keyUp"}
	for _, eventType := range events {
		payload := map[string]any{
			"type":                  eventType,
			"key":                   "Backspace",
			"code":                  "Backspace",
			"windowsVirtualKeyCode": 8,
			"nativeVirtualKeyCode":  8,
		}
		if err := client.Call(ctx, "Input.dispatchKeyEvent", payload, nil); err != nil {
			return err
		}
	}
	return nil
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func normalizeViewportSize(v viewportSize) viewportSize {
	out := v
	if out.Width < 64 {
		out.Width = 1280
	}
	if out.Height < 64 {
		out.Height = 720
	}
	return out
}

func clampPoint(p point2D, viewport viewportSize) point2D {
	v := normalizeViewportSize(viewport)
	return point2D{
		X: clampFloat(p.X, 1, v.Width-1),
		Y: clampFloat(p.Y, 1, v.Height-1),
	}
}

func cubicBezierPoint(p0, p1, p2, p3 point2D, t float64) point2D {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	mt := 1 - t
	mt2 := mt * mt
	t2 := t * t
	return point2D{
		X: (mt2*mt)*p0.X + (3*mt2*t)*p1.X + (3*mt*t2)*p2.X + (t2*t)*p3.X,
		Y: (mt2*mt)*p0.Y + (3*mt2*t)*p1.Y + (3*mt*t2)*p2.Y + (t2*t)*p3.Y,
	}
}

func distance(a, b point2D) float64 {
	return math.Hypot(a.X-b.X, a.Y-b.Y)
}

func clampFloat(value, min, max float64) float64 {
	if max < min {
		min, max = max, min
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func clampIntValue(value, min, max int) int {
	if max < min {
		min, max = max, min
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func roundToOneDecimal(value float64) float64 {
	return math.Round(value*10) / 10
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func maxIntValue(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func signedRandom(rng *rand.Rand, min, max float64) float64 {
	if rng == nil {
		return 0
	}
	if max < min {
		min, max = max, min
	}
	value := min + rng.Float64()*(max-min)
	if rng.Float64() < 0.5 {
		value = -value
	}
	return value
}

var qwertyNeighborMap = map[rune][]rune{
	'a': {'q', 'w', 's', 'z'},
	'b': {'v', 'g', 'h', 'n'},
	'c': {'x', 'd', 'f', 'v'},
	'd': {'s', 'e', 'r', 'f', 'c', 'x'},
	'e': {'w', 's', 'd', 'r'},
	'f': {'d', 'r', 't', 'g', 'v', 'c'},
	'g': {'f', 't', 'y', 'h', 'b', 'v'},
	'h': {'g', 'y', 'u', 'j', 'n', 'b'},
	'i': {'u', 'j', 'k', 'o'},
	'j': {'h', 'u', 'i', 'k', 'm', 'n'},
	'k': {'j', 'i', 'o', 'l', 'm'},
	'l': {'k', 'o', 'p'},
	'm': {'n', 'j', 'k'},
	'n': {'b', 'h', 'j', 'm'},
	'o': {'i', 'k', 'l', 'p'},
	'p': {'o', 'l'},
	'q': {'w', 'a'},
	'r': {'e', 'd', 'f', 't'},
	's': {'a', 'w', 'e', 'd', 'x', 'z'},
	't': {'r', 'f', 'g', 'y'},
	'u': {'y', 'h', 'j', 'i'},
	'v': {'c', 'f', 'g', 'b'},
	'w': {'q', 'a', 's', 'e'},
	'x': {'z', 's', 'd', 'c'},
	'y': {'t', 'g', 'h', 'u'},
	'z': {'a', 's', 'x'},
}

func nearbyTypoRune(r rune, rng *rand.Rand) (rune, bool) {
	if rng == nil {
		return 0, false
	}

	lower := unicode.ToLower(r)
	neighbors, ok := qwertyNeighborMap[lower]
	if !ok || len(neighbors) == 0 {
		return 0, false
	}

	choice := neighbors[rng.Intn(len(neighbors))]
	if unicode.IsUpper(r) {
		choice = unicode.ToUpper(choice)
	}
	return choice, true
}
