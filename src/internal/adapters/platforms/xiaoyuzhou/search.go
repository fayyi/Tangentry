package xiaoyuzhou

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"tangentry/internal/content"
	"tangentry/internal/discovery"
)

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
