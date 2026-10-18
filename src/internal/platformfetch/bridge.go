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
	Status      int
	APIStatus   int
	Parsed      bool
	Body        string
	Restricted  bool
	URL         string
	ContentType string
}

type Fetcher interface {
	Fetch(context.Context, string, string, Request) (Response, error)
}

// FeedCaptureResponse is returned by native shells that can observe the
// platform page's own feed requests. Responses are kept separate because the
// page may issue several feed calls while scrolling.
type FeedCaptureResponse struct {
	Responses          []Response
	FeedCards          []DOMCard
	FeedHasMore        bool
	FeedParsed         bool
	DOMCards           []DOMCard
	PageURL            string
	Title              string
	Ready              string
	ResourcePaths      []string
	InitiatorTypes     []string
	FetchHookInstalled bool
	XHRHookInstalled   bool
	DOM                DOMDiagnostics
}

// DOMDiagnostics describes the short, synchronous DOM probes performed by
// the native bridge. It is intentionally metadata-only: no raw HTML or
// cookies cross the bridge.
type DOMDiagnostics struct {
	URL                   string         `json:"url,omitempty"`
	VideoLinkCount        int            `json:"videoLinkCount,omitempty"`
	CandidateSelectorHits map[string]int `json:"candidateSelectorHits,omitempty"`
	CardsRead             int            `json:"cardsRead,omitempty"`
	WaitMilliseconds      int            `json:"waitMilliseconds,omitempty"`
}

// DOMCard is the credential-free projection of a recommendation card read
// from a platform page. It intentionally contains no cookies or raw HTML.
type DOMCard struct {
	ID           string `json:"id"`
	URL          string `json:"url"`
	ThumbnailURL string `json:"thumbnailURL"`
	Title        string `json:"title"`
	Author       string `json:"author"`
	AuthorID     string `json:"authorID"`
	LikeCount    int    `json:"likeCount"`
	Duration     int    `json:"duration"`
}

// FeedCaptureFetcher is optional. Adapters must continue to work with a
// regular Fetcher for server/headless builds and older shells.
type FeedCaptureFetcher interface {
	CaptureFeed(context.Context, string, int) (FeedCaptureResponse, error)
}

// SearchCaptureResponse is the safe card projection read from a platform's
// own search page in a native WebView.
type SearchCaptureResponse struct {
	DOMCards []DOMCard
	PageURL  string
	Title    string
	Ready    string
}

// SearchCaptureFetcher is optional and only available in desktop shells.
type SearchCaptureFetcher interface {
	CaptureSearch(context.Context, string, string, string) (SearchCaptureResponse, error)
}
