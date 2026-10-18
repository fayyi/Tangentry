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
	URL        string
}

type Fetcher interface {
	Fetch(context.Context, string, string, Request) (Response, error)
}

// FeedCaptureResponse is returned by native shells that can observe the
// platform page's own feed requests. Responses are kept separate because the
// page may issue several feed calls while scrolling.
type FeedCaptureResponse struct {
	Responses []Response
	PageURL   string
	Title     string
	Ready     string
}

// FeedCaptureFetcher is optional. Adapters must continue to work with a
// regular Fetcher for server/headless builds and older shells.
type FeedCaptureFetcher interface {
	CaptureFeed(context.Context, string, int) (FeedCaptureResponse, error)
}
