package main

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/VenkatGGG/Browser-use/internal/cdp"
)

type actionPlanner interface {
	Name() string
	Plan(ctx context.Context, goal string, snapshot pageSnapshot) ([]executeAction, error)
}

type heuristicPlanner struct{}

func newActionPlanner(mode string) actionPlanner {
	normalized := strings.ToLower(strings.TrimSpace(mode))
	switch normalized {
	case "", "heuristic":
		return &heuristicPlanner{}
	case "off":
		return nil
	default:
		return &heuristicPlanner{}
	}
}

func (p *heuristicPlanner) Name() string {
	return "heuristic"
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
		{Type: "wait", DelayMS: 1200},
	}
	return actions, nil
}

type pageSnapshot struct {
	URL      string        `json:"url"`
	Title    string        `json:"title"`
	Elements []pageElement `json:"elements"`
}

type pageElement struct {
	Tag         string `json:"tag"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	ID          string `json:"id"`
	Placeholder string `json:"placeholder"`
	AriaLabel   string `json:"aria_label"`
	Role        string `json:"role"`
	Text        string `json:"text"`
	Selector    string `json:"selector"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
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
	      width: Math.round(rect.width),
	      height: Math.round(rect.height)
	    });
    if (nodes.length >= 80) break;
  }

  return {
    url: String(window.location.href || ""),
    title: String(document.title || ""),
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
	return snapshot, nil
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
