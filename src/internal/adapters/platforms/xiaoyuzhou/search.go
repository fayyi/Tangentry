package xiaoyuzhou

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"

	"tangentry/internal/content"
	"tangentry/internal/discovery"
)

var podcastID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func (c *Client) Search(ctx context.Context, query, cursor string) (discovery.SearchPage, error) {
	access := ""
	if c.Auth != nil {
		access, _ = c.Auth.Tokens()
	}
	page := cursor
	if page == "" {
		page = "1"
	}
	body, _ := json.Marshal(map[string]any{"query": query, "page": page, "limit": 20})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.xiaoyuzhoufm.com/v1/search", bytes.NewReader(body))
	setAppHeaders(req, access)
	res, err := c.doAuthenticated(ctx, req, access)
	if err != nil {
		return discovery.SearchPage{}, discovery.ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return discovery.SearchPage{}, &discovery.PlatformError{Message: "小宇宙搜索需要有效的 App 登录态。", Restricted: true}
	}
	if res.StatusCode != http.StatusOK {
		return discovery.SearchPage{}, discovery.ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return discovery.SearchPage{}, err
	}
	var root struct {
		Data []any  `json:"data"`
		Next string `json:"nextCursor"`
	}
	if json.Unmarshal(raw, &root) != nil {
		return discovery.SearchPage{}, discovery.ErrUnavailable
	}
	items := make([]content.Item, 0, 20)
	seen := map[string]bool{}
	collectEpisodes(root.Data, &items, seen)
	return discovery.SearchPage{Items: items, Cursor: root.Next, HasMore: root.Next != "", Platform: "xiaoyuzhou"}, nil
}

func (c *Client) Creator(ctx context.Context, id, cursor string) (discovery.SearchPage, error) {
	id = strings.TrimSpace(id)
	if !podcastID.MatchString(id) || len(cursor) > 4096 {
		return discovery.SearchPage{}, discovery.ErrInvalidQuery
	}
	access := ""
	if c.Auth != nil {
		access, _ = c.Auth.Tokens()
	}
	payload := map[string]any{"pid": id, "order": "desc", "limit": 20}
	if cursor != "" {
		payload["loadMoreKey"] = decodeLoadMoreKey(cursor)
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.xiaoyuzhoufm.com/v1/episode/list", bytes.NewReader(body))
	setAppHeaders(req, access)
	res, err := c.doAuthenticated(ctx, req, access)
	if err != nil {
		if ctx.Err() != nil {
			return discovery.SearchPage{}, ctx.Err()
		}
		return discovery.SearchPage{}, discovery.ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return discovery.SearchPage{}, &discovery.PlatformError{Message: "小宇宙节目单集需要有效的 App 登录态。", Restricted: true}
	}
	if res.StatusCode != http.StatusOK {
		return discovery.SearchPage{}, discovery.ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return discovery.SearchPage{}, err
	}
	var root struct {
		Data any             `json:"data"`
		Next json.RawMessage `json:"loadMoreKey"`
	}
	if json.Unmarshal(raw, &root) != nil {
		return discovery.SearchPage{}, discovery.ErrUnavailable
	}
	items := make([]content.Item, 0, 20)
	seen := map[string]bool{}
	collectEpisodes(root.Data, &items, seen)
	// The list endpoint is scoped by pid, but older responses occasionally omit
	// the nested podcast object. Keep only records that belong to this program
	// and fill the stable creator identity used by the temporary creator view.
	creatorItems := items[:0]
	for _, item := range items {
		if item.AuthorID != "" && item.AuthorID != id {
			continue
		}
		item.AuthorID = id
		creatorItems = append(creatorItems, item)
	}
	next := encodeLoadMoreKey(root.Next)
	return discovery.SearchPage{Items: creatorItems, Cursor: next, HasMore: next != "", Platform: "xiaoyuzhou"}, nil
}

// decodeLoadMoreKey preserves object cursors returned by the app API while
// remaining compatible with older deployments that use a plain string.
func decodeLoadMoreKey(cursor string) any {
	var value any
	if json.Unmarshal([]byte(cursor), &value) == nil {
		if _, ok := value.(map[string]any); ok {
			return value
		}
	}
	return cursor
}

func encodeLoadMoreKey(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	if text, ok := value.(string); ok {
		if len(text) > 4096 {
			return ""
		}
		return text
	}
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > 4096 {
		return ""
	}
	return string(encoded)
}
