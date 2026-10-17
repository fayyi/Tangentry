package youtube

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"regexp"
	"time"
)

const (
	homeResultLimit      = 200
	homeMaxContinuations = 2
	continuationLifetime = 20 * time.Minute
)

// HomeResult mirrors the former Node helper protocol: entries with a mode, or
// an error code the toolkit turns into a user-facing diagnosis.
type HomeResult struct {
	Mode         string
	Entries      []HomeEntry
	Continuation json.RawMessage
	Error        string
}

// Home reads YouTube home recommendations through InnerTube JSON requests.
// It keeps no session cache, player code or OAuth tokens; the caller supplies
// the scoped cookie header for each request.
type Home struct {
	HTTP *http.Client
	Now  func() time.Time
}

// NewHome returns a client that refuses redirects, so cookies never leave the
// YouTube API origin.
func NewHome() *Home {
	return &Home{HTTP: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// continuationState is internal adapter state, never an arbitrary URL or a
// browser-supplied request.
type continuationState struct {
	Token        string          `json:"token"`
	Context      json.RawMessage `json:"context"`
	APIKey       string          `json:"apiKey"`
	APIVersion   string          `json:"apiVersion"`
	AccountIndex float64         `json:"accountIndex"`
	Mode         string          `json:"mode"`
	ExpiresAt    float64         `json:"expiresAt"`
}

var apiVersionPattern = regexp.MustCompile(`^v\d+$`)

func (s continuationState) session(now time.Time) (session, bool) {
	limit := float64(now.Add(continuationLifetime).UnixMilli())
	if s.Token == "" || len(s.Token) > 16384 || math.IsNaN(s.ExpiresAt) || s.ExpiresAt <= float64(now.UnixMilli()) || s.ExpiresAt > limit {
		return session{}, false
	}
	if s.Mode != "account" && s.Mode != "anonymous" && s.Mode != "subscriptions" {
		return session{}, false
	}
	if len(s.APIKey) > 256 || !apiVersionPattern.MatchString(s.APIVersion) || s.AccountIndex != math.Trunc(s.AccountIndex) || s.AccountIndex < 0 || s.AccountIndex > 99 {
		return session{}, false
	}
	var context map[string]any
	if len(s.Context) > 32768 || json.Unmarshal(s.Context, &context) != nil || path(context, "client", "clientName") != "WEB" {
		return session{}, false
	}
	return session{Context: context, APIKey: s.APIKey, APIVersion: s.APIVersion, AccountIndex: int(s.AccountIndex)}, true
}

func captureContinuation(page feedPage, s session, mode string, now time.Time) json.RawMessage {
	if page.continuation == "" {
		return nil
	}
	context, err := json.Marshal(s.Context)
	if err != nil {
		return nil
	}
	state, err := json.Marshal(continuationState{Token: page.continuation, Context: context, APIKey: s.APIKey, APIVersion: s.APIVersion,
		AccountIndex: float64(s.AccountIndex), Mode: mode, ExpiresAt: float64(now.Add(continuationLifetime).UnixMilli())})
	if err != nil {
		return nil
	}
	return state
}

// Fetch reads one batch: the home feed (falling back to account subscription
// uploads), or the page after a saved continuation.
func (h *Home) Fetch(ctx context.Context, cookie string, continuation json.RawMessage) HomeResult {
	now := time.Now
	if h.Now != nil {
		now = h.Now
	}
	r := &requester{http: h.HTTP, cookie: cookie, now: now, code: "network"}
	var (
		s       session
		entries []HomeEntry
		last    feedPage
		mode    string
		err     error
	)
	if continuation != nil {
		var state continuationState
		var ok bool
		if json.Unmarshal(continuation, &state) == nil {
			s, ok = state.session(now())
		}
		if !ok {
			return HomeResult{Error: "continuation_expired"}
		}
		mode = state.Mode
		var data map[string]any
		if data, err = r.browse(ctx, s, map[string]any{"continuation": state.Token}); err == nil {
			entries, last, err = r.collect(ctx, s, parseFeedPage(data))
		}
	} else {
		s = r.newSession(ctx)
		var diagnosis string
		entries, last, mode, diagnosis, err = r.resolveHome(ctx, s)
		if err == nil && diagnosis != "" {
			return HomeResult{Error: diagnosis}
		}
	}
	if err != nil {
		var failure *innertubeError
		if errors.As(err, &failure) && failure.code == "batch_size" {
			return HomeResult{Error: "batch_size"}
		}
		return HomeResult{Error: r.code}
	}
	return HomeResult{Mode: mode, Entries: entries, Continuation: captureContinuation(last, s, mode, now())}
}

// collect follows a bounded number of continuations when a browse response
// contains only shelves or Shorts, and returns the last page it read.
func (r *requester) collect(ctx context.Context, s session, page feedPage) ([]HomeEntry, feedPage, error) {
	var cards []card
	for index := 0; ; index++ {
		cards = append(cards, page.cards...)
		entries, err := normalizeCards(cards, homeResultLimit)
		if err != nil {
			return nil, page, err
		}
		if len(entries) > 0 || !page.more || page.continuation == "" || index == homeMaxContinuations {
			return entries, page, nil
		}
		data, err := r.browse(ctx, s, map[string]any{"continuation": page.continuation})
		if err != nil {
			return nil, page, err
		}
		page = parseFeedPage(data)
	}
}

// resolveHome returns account recommendations, falling back to subscription
// uploads for a cookie session. A secondary failure never replaces a known
// home diagnosis, and rejected cookies or oversized batches stop immediately.
func (r *requester) resolveHome(ctx context.Context, s session) ([]HomeEntry, feedPage, string, string, error) {
	var homeErr error
	nudge := false
	data, err := r.browse(ctx, s, map[string]any{"browseId": "FEwhat_to_watch"})
	if err == nil {
		page := parseFeedPage(data)
		nudge = page.nudge
		var entries []HomeEntry
		var last feedPage
		if entries, last, err = r.collect(ctx, s, page); err == nil && len(entries) > 0 {
			mode := "anonymous"
			if r.cookie != "" {
				mode = "account"
			}
			return entries, last, mode, "", nil
		}
	}
	if err != nil {
		if hardError(err) {
			return nil, feedPage{}, "", "", err
		}
		homeErr = err
	}
	if r.cookie != "" {
		data, err := r.browse(ctx, s, map[string]any{"browseId": "FEsubscriptions"})
		var entries []HomeEntry
		var last feedPage
		if err == nil {
			entries, last, err = r.collect(ctx, s, parseFeedPage(data))
		}
		if err == nil && len(entries) > 0 {
			return entries, last, "subscriptions", "", nil
		}
		if err != nil && (hardError(err) || (!nudge && homeErr == nil)) {
			return nil, feedPage{}, "", "", err
		}
	}
	if homeErr != nil {
		return nil, feedPage{}, "", "", homeErr
	}
	if nudge {
		return nil, feedPage{}, "", "home_nudge", nil
	}
	return nil, feedPage{}, "", "empty", nil
}
