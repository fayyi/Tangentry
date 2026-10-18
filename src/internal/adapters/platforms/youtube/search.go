package youtube

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"tangentry/internal/content"
	"tangentry/internal/discovery"
)

// Search uses the same InnerTube session/signing path as the home feed. The
// first page is intentionally bounded; pagination uses the returned opaque
// continuation token.
func (h *Home) Search(ctx context.Context, query, cursor string) (discovery.SearchPage, error) {
	return h.SearchWithCookie(ctx, "", query, cursor)
}

func (h *Home) SearchWithCookie(ctx context.Context, cookie, query, cursor string) (discovery.SearchPage, error) {
	if query == "" {
		return discovery.SearchPage{}, discovery.ErrInvalidQuery
	}
	now := time.Now
	if h.Now != nil {
		now = h.Now
	}
	r := &requester{http: h.HTTP, cookie: cookie, now: now, userAgent: h.UserAgent}
	session := r.newSession(ctx)
	var data map[string]any
	var err error
	if cursor != "" {
		var state continuationState
		if json.Unmarshal([]byte(cursor), &state) != nil {
			return discovery.SearchPage{}, discovery.ErrInvalidQuery
		}
		var ok bool
		session, ok = state.session(now())
		if !ok {
			return discovery.SearchPage{}, &discovery.PlatformError{Message: "YouTube 搜索会话已过期，请重新搜索。"}
		}
		data, err = r.browse(ctx, session, map[string]any{"continuation": state.Token})
	} else {
		data, err = r.search(ctx, session, query)
	}
	if err != nil {
		return discovery.SearchPage{}, &discovery.PlatformError{Message: "YouTube 搜索暂时不可用，请稍后重试。"}
	}
	entries, page, err := r.collect(ctx, session, parseFeedPage(data))
	if err != nil {
		return discovery.SearchPage{}, err
	}
	items := make([]content.Item, 0, len(entries))
	for _, entry := range entries {
		id, canonical, parseErr := discovery.ParseYouTube("https://www.youtube.com/watch?v=" + entry.ID)
		if parseErr != nil {
			continue
		}
		item := content.Item{ID: "youtube:" + id, Platform: "youtube", PlatformID: id, Kind: "video", Title: entry.Title, Author: entry.Author, AuthorID: entry.AuthorID, URL: canonical, ThumbnailURL: entry.Thumbnail, DurationSeconds: entry.Duration, PublishedText: entry.PublishedText}
		if content.Validate(item) == nil {
			items = append(items, item)
		}
	}
	var next string
	if page.continuation != "" {
		raw, _ := json.Marshal(continuationState{Token: page.continuation, Context: mustJSON(session.Context), APIKey: session.APIKey, APIVersion: session.APIVersion, AccountIndex: float64(session.AccountIndex), Mode: "anonymous", ExpiresAt: float64(now().Add(20 * time.Minute).UnixMilli())})
		next = string(raw)
	}
	return discovery.SearchPage{Items: items, Cursor: next, HasMore: next != "", Platform: "youtube"}, nil
}

func (r *requester) search(ctx context.Context, s session, query string) (map[string]any, error) {
	payload := map[string]any{"query": query}
	payload["context"] = s.Context
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	endpoint := &url.URL{Scheme: "https", Host: "www.youtube.com", Path: "/youtubei/" + s.APIVersion + "/search", RawQuery: "prettyPrint=false&alt=json"}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client, _ := s.Context["client"].(map[string]any)
	req.Header.Set("User-Agent", firstNonEmpty(stringAt(client, "userAgent"), r.userAgent, innertubeUserAgent))
	req.Header.Set("Origin", innertubeBase)
	req.Header.Set("X-Goog-Visitor-Id", stringAt(client, "visitorData"))
	req.Header.Set("X-Youtube-Client-Version", stringAt(client, "clientVersion"))
	req.Header.Set("X-Youtube-Client-Name", "1")
	signRequest(req.Header, req.URL, r.cookie, r.now())
	raw, err := r.do(ctx, req)
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	return data, nil
}

func mustJSON(value map[string]any) json.RawMessage {
	raw, _ := json.Marshal(value)
	return raw
}
