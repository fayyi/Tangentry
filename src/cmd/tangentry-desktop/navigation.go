package main

import "net/url"

// navigationDecision keeps WebView navigation policy independent from MyGo's
// event type. The desktop shell owns the policy; tests can exercise it without
// creating a native window.
type navigationDecision struct {
	allow          bool
	openExternally bool
}

func decideNavigation(rawURL, ownOrigin string, userInitiated bool) navigationDecision {
	u, err := url.Parse(rawURL)
	if err != nil {
		return navigationDecision{}
	}
	if u.Scheme+"://"+u.Host == ownOrigin {
		return navigationDecision{allow: true}
	}
	if !userInitiated {
		return navigationDecision{}
	}
	switch u.Scheme {
	case "http", "https", "mailto":
		return navigationDecision{openExternally: true}
	default:
		return navigationDecision{}
	}
}
