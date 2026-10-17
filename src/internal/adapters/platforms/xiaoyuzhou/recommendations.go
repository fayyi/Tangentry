package xiaoyuzhou

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"time"
)

type recommendationState struct {
	mu      sync.Mutex
	expires time.Time // Protected by the client's recommendationMu.
	pages   map[int]discovery.Feed
	next    string
	seen    map[string]bool
	cursors map[string]bool
}

func (c *Client) recommendationState(ctx context.Context, page int) (*recommendationState, error) {
	key := discovery.RecommendationSession(ctx)
	if len(key) > 128 {
		return nil, discovery.ErrInvalidQuery
	}
	c.recommendationMu.Lock()
	defer c.recommendationMu.Unlock()
	now := time.Now()
	for key, state := range c.recommendationSessions {
		if !state.expires.After(now) {
			delete(c.recommendationSessions, key)
		}
	}
	if c.recommendationSessions == nil {
		c.recommendationSessions = map[string]*recommendationState{}
	}
	state := c.recommendationSessions[key]
	if state == nil {
		if page != 1 {
			return nil, &discovery.PlatformError{Message: "小宇宙推荐会话已过期，请刷新推荐。"}
		}
		if len(c.recommendationSessions) >= 8 {
			return nil, &discovery.PlatformError{Message: "推荐浏览窗口较多，请稍后刷新重试。"}
		}
		state = &recommendationState{}
		c.recommendationSessions[key] = state
	}
	state.expires = now.Add(19 * time.Minute)
	return state, nil
}

// Recommendations reads the official discovery feed. The mobile API returns
// several editorial sections; only entries with a concrete episode ID are
// exposed as audio cards, so headers and category decorations never become
// fake content.
func (c *Client) Recommendations(ctx context.Context, page int, token string) (discovery.Feed, error) {
	if page < 1 || page > 64 {
		return discovery.Feed{}, discovery.ErrInvalidQuery
	}
	if err := ctx.Err(); err != nil {
		return discovery.Feed{}, err
	}
	state, err := c.recommendationState(ctx, page)
	if err != nil {
		return discovery.Feed{}, err
	}
	if !state.mu.TryLock() {
		return discovery.Feed{}, &discovery.PlatformError{Message: "小宇宙推荐正在获取，请稍后重试。"}
	}
	defer state.mu.Unlock()
	if page > 1 {
		if cached, ok := state.pages[page]; ok {
			return cached, nil
		}
		if page != len(state.pages)+1 || state.next == "" {
			return discovery.Feed{}, &discovery.PlatformError{Message: "小宇宙推荐没有可用的下一页，请刷新推荐。"}
		}
	}
	input := map[string]string{"returnAll": "false"}
	if page > 1 {
		input["loadMoreKey"] = state.next
	}
	body, _ := json.Marshal(input)
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://api.xiaoyuzhoufm.com/v1/discovery-feed/list", bytes.NewReader(body))
	setAppHeaders(req, token)
	res, err := c.doAuthenticated(ctx, req, token)
	if err != nil {
		return discovery.Feed{}, discovery.ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode == 401 || res.StatusCode == 403 {
		return discovery.Feed{}, &discovery.PlatformError{Message: "小宇宙推荐需要有效的 App 访问令牌。", Restricted: true}
	}
	if res.StatusCode != 200 {
		return discovery.Feed{}, discovery.ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, (4<<20)+1))
	if err != nil || len(raw) == 0 || len(raw) > 4<<20 {
		return discovery.Feed{}, discovery.ErrUnavailable
	}
	var root struct {
		Data []any  `json:"data"`
		Next string `json:"loadMoreKey"`
	}
	if json.Unmarshal(raw, &root) != nil || root.Data == nil || len(root.Next) > 4096 {
		return discovery.Feed{}, discovery.ErrUnavailable
	}
	items := make([]content.Item, 0, 20)
	seen := map[string]bool{}
	if page > 1 {
		for id := range state.seen {
			seen[id] = true
		}
	}
	collectEpisodes(root.Data, &items, seen)
	if err := ctx.Err(); err != nil {
		return discovery.Feed{}, err
	}
	if page == 1 {
		state.pages = map[int]discovery.Feed{}
		state.cursors = map[string]bool{}
	}
	next := root.Next
	if page == 64 || state.cursors[next] {
		next = ""
	}
	feed := discovery.Feed{Items: items, Page: page, Total: len(seen), HasMore: next != "", SourceTitle: "小宇宙 · 发现推荐"}
	state.pages[page] = feed
	state.next = next
	state.seen = seen
	if next != "" {
		state.cursors[next] = true
	}
	return feed, nil
}
