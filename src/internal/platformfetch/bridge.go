// Package platformfetch defines the small port used by platform adapters to
// request same-origin data through a native authenticated WebView.
package platformfetch

import "context"

type Request struct {
	Method  string
	Headers map[string]string
	Body    string
}

type Response struct {
	Status     int
	Body       string
	Restricted bool
}

type Fetcher interface {
	Fetch(context.Context, string, string, Request) (Response, error)
}
