package main

import "testing"

func TestClassifyBlocker(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		url   string
		title string
		body  string
		want  string
	}{
		{
			name:  "captcha challenge",
			url:   "https://duckduckgo.com/?q=browser+use",
			title: "DuckDuckGo",
			body:  "Please complete the following challenge to confirm this search was made by a human.",
			want:  "human_verification_required",
		},
		{
			name:  "form validation",
			url:   "https://duckduckgo.com",
			title: "DuckDuckGo",
			body:  "Please fill out this field.",
			want:  "form_validation_error",
		},
		{
			name:  "bot blocked",
			url:   "https://example.com/protected",
			title: "Access denied",
			body:  "Access denied. Suspicious bot traffic detected.",
			want:  "bot_blocked",
		},
		{
			name:  "normal page",
			url:   "https://example.com",
			title: "Example Domain",
			body:  "This domain is for use in illustrative examples.",
			want:  "",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, _ := classifyBlocker(tc.url, tc.title, tc.body)
			if got != tc.want {
				t.Fatalf("classifyBlocker()=%q want=%q", got, tc.want)
			}
		})
	}
}
