package main

import "testing"

func TestDecideNavigation(t *testing.T) {
	const origin = "http://127.0.0.1:7380"
	for _, test := range []struct {
		name          string
		rawURL        string
		userInitiated bool
		want          navigationDecision
	}{
		{name: "own page", rawURL: origin + "/reader", want: navigationDecision{allow: true}},
		{name: "own page query", rawURL: origin + "/?view=notes", userInitiated: true, want: navigationDecision{allow: true}},
		{name: "clicked web link", rawURL: "https://example.com/article", userInitiated: true, want: navigationDecision{openExternally: true}},
		{name: "clicked mail link", rawURL: "mailto:hello@example.com", userInitiated: true, want: navigationDecision{openExternally: true}},
		{name: "script redirect", rawURL: "https://example.com/redirect", want: navigationDecision{}},
		{name: "unsupported scheme", rawURL: "javascript:alert(1)", userInitiated: true, want: navigationDecision{}},
		{name: "malformed URL", rawURL: "http://[", userInitiated: true, want: navigationDecision{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := decideNavigation(test.rawURL, origin, test.userInitiated); got != test.want {
				t.Fatalf("decideNavigation(%q, %q, %v) = %+v, want %+v", test.rawURL, origin, test.userInitiated, got, test.want)
			}
		})
	}
}
