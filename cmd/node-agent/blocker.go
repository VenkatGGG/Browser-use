package main

import "strings"

func classifyBlocker(url, title, bodyText string) (string, string) {
	haystack := strings.ToLower(strings.Join([]string{
		strings.TrimSpace(url),
		strings.TrimSpace(title),
		strings.TrimSpace(bodyText),
	}, " "))

	if haystack == "" {
		return "", ""
	}

	humanSignals := []string{
		"captcha",
		"hcaptcha",
		"recaptcha",
		"verify you are human",
		"prove you are human",
		"are you a robot",
		"confirm this search was made by a human",
		"complete the following challenge",
		"security check",
		"checking if the site connection is secure",
	}
	for _, signal := range humanSignals {
		if strings.Contains(haystack, signal) {
			return "human_verification_required", "human verification challenge detected"
		}
	}

	if strings.Contains(haystack, "please fill out this field") {
		return "form_validation_error", "submission blocked by required-field validation"
	}

	if strings.Contains(haystack, "access denied") && strings.Contains(haystack, "bot") {
		return "bot_blocked", "target denied automated access"
	}

	return "", ""
}
