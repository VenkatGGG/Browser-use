package main

import "strings"

func classifyBlocker(url, title, bodyText string) (string, string) {
	haystack := blockerHaystack(url, title, bodyText)
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
		"please stand by while we check your browser",
		"attention required",
	}
	botSignals := []string{
		"access denied",
		"bot detected",
		"automated access",
		"suspicious traffic",
		"unusual traffic",
		"request blocked",
		"forbidden",
		"error 1020",
	}
	for _, signal := range botSignals {
		if strings.Contains(haystack, signal) {
			return "bot_blocked", "target denied automated access"
		}
	}

	for _, signal := range humanSignals {
		if strings.Contains(haystack, signal) {
			return "human_verification_required", "human verification challenge detected"
		}
	}

	if strings.Contains(haystack, "please fill out this field") {
		return "form_validation_error", "submission blocked by required-field validation"
	}

	return "", ""
}

func isLikelyTransientChallenge(url, title, bodyText string) bool {
	haystack := blockerHaystack(url, title, bodyText)
	if haystack == "" {
		return false
	}
	transientSignals := []string{
		"checking if the site connection is secure",
		"checking your browser before accessing",
		"just a moment",
		"enable javascript and cookies to continue",
		"ddos protection by cloudflare",
	}
	for _, signal := range transientSignals {
		if strings.Contains(haystack, signal) {
			return true
		}
	}
	return false
}

func blockerHaystack(url, title, bodyText string) string {
	return strings.ToLower(strings.Join([]string{
		strings.TrimSpace(url),
		strings.TrimSpace(title),
		strings.TrimSpace(bodyText),
	}, " "))
}
