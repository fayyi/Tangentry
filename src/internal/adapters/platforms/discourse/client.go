// Package discourse reads Linux.do topic feeds using the owner's site session.
package discourse

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"tangentry/internal/adapters/feeds"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"tangentry/internal/platformfetch"
)

type Client struct {
	HTTP      *http.Client
	Cookie    func(string) string
	UserAgent func(string) string
	Session   func() (string, func([]*http.Cookie) error)
	UserKey   func() (string, string)
	Page      platformfetch.Fetcher
}

type SessionState struct {
	LoggedIn bool   `json:"loggedIn"`
	Username string `json:"username,omitempty"`
}

const discourseBrowserUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36"

func setDiscourseBrowserHeaders(req *http.Request, ajax bool) {
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("User-Agent", discourseBrowserUserAgent)
	req.Header.Set("Referer", "https://linux.do/")
	if ajax {
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		req.Header.Set("Sec-Fetch-Dest", "empty")
		req.Header.Set("Sec-Fetch-Mode", "cors")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
	}
}

func (c *Client) read(ctx context.Context, path string) ([]byte, int, error) {
	if c.Page != nil {
		response, err := c.Page.Fetch(ctx, "linuxdo", path, platformfetch.Request{Method: http.MethodGet, Headers: map[string]string{
			"Accept":           "application/json, text/javascript, */*; q=0.01",
			"X-Requested-With": "XMLHttpRequest",
		}})
		if err == nil {
			raw := []byte(response.Body)
			if response.Restricted || response.Status == http.StatusForbidden || response.Status == http.StatusUnauthorized || response.Status == http.StatusTooManyRequests || looksLikeCloudflare(raw) {
				return nil, response.Status, linuxdoPageRestricted(response.Status)
			}
			if response.Status != http.StatusOK {
				if response.Status == http.StatusNotFound {
					return nil, response.Status, nil
				}
				return nil, response.Status, discovery.ErrUnavailable
			}
			if !json.Valid(raw) {
				if response.Restricted || looksLikeCloudflare(raw) {
					return nil, response.Status, linuxdoPageRestricted(response.Status)
				}
				return nil, response.Status, discovery.ErrUnavailable
			}
			return raw, response.Status, nil
		}
		// A native bridge is optional. If it cannot create or use a WebView,
		// retain the server-side HTTP fallback below.
	}
	if c.HTTP == nil {
		return nil, 0, discovery.ErrUnavailable
	}
	origin := "https://linux.do" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin, nil)
	if err != nil {
		return nil, 0, discovery.ErrUnavailable
	}
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	setDiscourseBrowserHeaders(req, true)
	if c.UserAgent != nil {
		if userAgent := c.UserAgent(origin); userAgent != "" {
			req.Header.Set("User-Agent", userAgent)
		}
	}
	var save func([]*http.Cookie) error
	cookieHeader := ""
	if c.Session != nil {
		header, commit := c.Session()
		cookieHeader = header
		save = commit
	} else if c.Cookie != nil {
		cookieHeader = c.Cookie(origin)
	}
	if cookieHeader != "" {
		req.Header.Set("Cookie", cookieHeader)
	}
	if c.UserKey != nil {
		key, clientID := c.UserKey()
		// A User-Api-Key is a device-authorisation credential, not a browser
		// session. Prefer the real _t/cf_clearance pair whenever available;
		// use the key only for key-only installations and the session probe.
		if key != "" && (cookieHeader == "" || path == "/session/current.json") {
			req.Header.Del("Cookie")
			req.Header.Set("User-Api-Key", key)
			req.Header.Set("User-Api-Client-Id", clientID)
			if cookieHeader == "" {
				save = nil
			}
		}
	}
	client := *c.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		return nil, 0, discovery.ErrUnavailable
	}
	defer res.Body.Close()
	if strings.EqualFold(res.Header.Get("cf-mitigated"), "challenge") {
		return nil, res.StatusCode, &discovery.PlatformError{Message: "Linux.do 触发 Cloudflare 验证，请在桌面浏览器打开 linux.do 完成验证后重新导入 Cookie（需包含 cf_clearance）。", Restricted: true}
	}
	switch res.StatusCode {
	case 401:
		return nil, res.StatusCode, &discovery.PlatformError{Message: "Linux.do 登录已失效，请重新保存原站登录信息。", Restricted: true}
	case 403:
		return nil, res.StatusCode, &discovery.PlatformError{Message: "Linux.do 触发 Cloudflare 验证，请在桌面浏览器打开 linux.do 完成验证后重新导入 Cookie（需包含 cf_clearance）。", Restricted: true}
	case 429:
		return nil, res.StatusCode, &discovery.PlatformError{Message: "Linux.do 暂时限制请求，请稍后手动重试。", Restricted: true}
	}
	if res.StatusCode != http.StatusOK {
		return nil, res.StatusCode, discovery.ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if err != nil || len(raw) > 2<<20 || !json.Valid(raw) {
		if looksLikeCloudflare(raw) {
			return nil, res.StatusCode, &discovery.PlatformError{Message: "Linux.do 返回了 Cloudflare 验证页面，请在原站完成验证后重试。", Restricted: true}
		}
		return nil, res.StatusCode, discovery.ErrUnavailable
	}
	if save != nil && req.Header.Get("Cookie") != "" {
		if err := save(res.Cookies()); err != nil {
			return nil, res.StatusCode, discovery.ErrUnavailable
		}
	}
	return raw, res.StatusCode, nil
}

func linuxdoPageRestricted(status int) error {
	if status == http.StatusUnauthorized {
		return &discovery.PlatformError{Message: "Linux.do 登录已失效，请在应用内完成验证后重试。", Restricted: true}
	}
	if status == http.StatusTooManyRequests {
		return &discovery.PlatformError{Message: "Linux.do 暂时限制请求，请稍后在应用内重试。", Restricted: true}
	}
	return &discovery.PlatformError{Message: "需要在应用内完成 Linux.do 验证", Restricted: true}
}

func looksLikeCloudflare(raw []byte) bool {
	value := strings.ToLower(string(raw))
	return strings.Contains(value, "cf-chl-") || strings.Contains(value, "challenge-platform") || strings.Contains(value, "just a moment") || strings.Contains(value, "cloudflare")
}

func (c *Client) CurrentSession(ctx context.Context) (SessionState, error) {
	raw, status, err := c.read(ctx, "/session/current.json")
	if status == 404 || status == 401 {
		return SessionState{}, nil
	}
	if err != nil {
		return SessionState{}, err
	}
	var data struct {
		User *struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
		} `json:"current_user"`
	}
	if json.Unmarshal(raw, &data) != nil {
		return SessionState{}, discovery.ErrUnavailable
	}
	if data.User == nil {
		return SessionState{}, nil
	}
	if data.User.ID <= 0 || strings.TrimSpace(data.User.Username) == "" {
		return SessionState{}, discovery.ErrUnavailable
	}
	return SessionState{LoggedIn: true, Username: data.User.Username}, nil
}

func ParseTopicURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "linux.do" || u.User != nil {
		return "", discovery.ErrInvalidQuery
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] != "t" {
		return "", discovery.ErrInvalidQuery
	}
	index := 1
	if _, err := strconv.ParseInt(parts[index], 10, 64); err != nil {
		index = 2
	}
	if len(parts) <= index {
		return "", discovery.ErrInvalidQuery
	}
	id := strings.TrimSuffix(parts[index], ".json")
	number, err := strconv.ParseInt(id, 10, 64)
	if err != nil || number <= 0 {
		return "", discovery.ErrInvalidQuery
	}
	return strconv.FormatInt(number, 10), nil
}

func (c *Client) Topic(ctx context.Context, id string) (content.Item, error) {
	number, err := strconv.ParseInt(id, 10, 64)
	if err != nil || number <= 0 {
		return content.Item{}, discovery.ErrInvalidQuery
	}
	raw, _, err := c.read(ctx, "/t/"+id+".json")
	if err != nil {
		return content.Item{}, err
	}
	var data struct {
		ID         int64  `json:"id"`
		Title      string `json:"title"`
		Created    string `json:"created_at"`
		Category   string `json:"category_name"`
		Views      int    `json:"views"`
		Likes      int    `json:"like_count"`
		ReplyCount int    `json:"reply_count"`
		Stream     struct {
			Posts []struct {
				Number   int    `json:"post_number"`
				Body     string `json:"cooked"`
				Username string `json:"username"`
				Avatar   string `json:"avatar_template"`
			} `json:"posts"`
		} `json:"post_stream"`
	}
	if json.Unmarshal(raw, &data) != nil || data.ID != number {
		return content.Item{}, discovery.ErrUnavailable
	}
	date, _ := time.Parse(time.RFC3339Nano, data.Created)
	item := content.Item{ID: "linuxdo:" + id, Platform: "linuxdo", PlatformID: id, Kind: "article", Title: data.Title, Category: data.Category, ViewCount: int64(data.Views), LikeCount: data.Likes, URL: "https://linux.do/t/" + id}
	if !date.IsZero() {
		item.PublishedAt = date.Unix()
	}
	var body strings.Builder
	blocks := make([]content.ArticleBlock, 0, 32)
	hasImage := false
	for _, post := range data.Stream.Posts {
		text := feeds.Text(post.Body)
		cookedBlocks := feeds.ParseArticleBlocks(post.Body, item.URL)
		if text == "" && len(cookedBlocks) == 0 {
			continue
		}
		if post.Number == 1 {
			item.Author = post.Username
			item.AuthorAvatarURL = discourseAvatar(post.Avatar)
		}
		if body.Len() > 0 {
			body.WriteString("\n\n")
		}
		if post.Number > 1 && post.Username != "" {
			body.WriteString("回复 ")
			body.WriteString(post.Username)
			body.WriteString("：")
			blocks = append(blocks, content.ArticleBlock{Kind: "paragraph", Text: "回复 " + post.Username + "："})
		}
		body.WriteString(text)
		for _, block := range cookedBlocks {
			if block.Kind == "image" {
				hasImage = true
			}
		}
		blocks = append(blocks, cookedBlocks...)
	}
	item.Description = feeds.Text(body.String())
	if data.ReplyCount > 0 {
		item.ReplyCount = data.ReplyCount
	} else if len(data.Stream.Posts) > 1 {
		item.ReplyCount = len(data.Stream.Posts) - 1
	}
	if item.Description != "" || len(blocks) > 0 {
		if !hasImage {
			blocks = []content.ArticleBlock{{Kind: "paragraph", Text: item.Description}}
		}
		item.ArticleBlocks = blocks
		if len(item.ArticleBlocks) == 0 {
			item.ArticleBlocks = []content.ArticleBlock{{Kind: "paragraph", Text: item.Description}}
		}
	}
	if content.Validate(item) != nil {
		return content.Item{}, discovery.ErrUnavailable
	}
	return item, nil
}

func (c *Client) Latest(ctx context.Context, page int) (discovery.Feed, error) {
	return c.topics(ctx, "latest", page)
}

// Hot is the site's ranked feed. Fall back only when the site lacks this route,
// never when authentication, verification or rate limiting blocks it.
func (c *Client) Recommendations(ctx context.Context, page int) (discovery.Feed, error) {
	// Keep the legacy hot route as a compatibility fallback for older Linux.do
	// deployments; Feed("top", ...) uses the current native /top.json route.
	feed, status, err := c.readTopics(ctx, "hot", page)
	if status == 404 {
		return c.Latest(ctx, page)
	}
	return feed, err
}

// Feed returns one of Discourse's native topic lists. Filters are deliberately
// kept server-side so pagination remains stable across refreshes.
func (c *Client) Feed(ctx context.Context, filter string, page int) (discovery.Feed, error) {
	if filter == "hot" {
		return c.Recommendations(ctx, page)
	}
	if filter != "latest" && filter != "top" && filter != "new" && filter != "unread" {
		return discovery.Feed{}, discovery.ErrInvalidQuery
	}
	feed, status, err := c.readTopics(ctx, filter, page)
	if filter == "top" && status == http.StatusNotFound {
		return c.Latest(ctx, page)
	}
	return feed, err
}

func (c *Client) topics(ctx context.Context, filter string, page int) (discovery.Feed, error) {
	feed, _, err := c.readTopics(ctx, filter, page)
	return feed, err
}

func (c *Client) readTopics(ctx context.Context, filter string, page int) (discovery.Feed, int, error) {
	if page < 1 || page > 10000 {
		return discovery.Feed{}, 0, discovery.ErrInvalidQuery
	}
	raw, status, err := c.read(ctx, "/"+filter+".json?page="+strconv.Itoa(page-1))
	if err != nil {
		return discovery.Feed{}, status, err
	}
	feed, err := Parse(raw, page)
	switch filter {
	case "hot", "top":
		feed.SourceTitle = "Linux.do · 热门话题"
	case "new":
		feed.SourceTitle = "Linux.do · 新话题"
	case "unread":
		feed.SourceTitle = "Linux.do · 未读话题"
	}
	return feed, status, err
}

func Parse(raw []byte, page int) (discovery.Feed, error) {
	var data struct {
		TopicList *struct {
			More   string `json:"more_topics_url"`
			Topics []struct {
				ID       int64  `json:"id"`
				Title    string `json:"title"`
				Created  string `json:"created_at"`
				Excerpt  string `json:"excerpt"`
				Category string `json:"category_name"`
				Author   string `json:"last_poster_username"`
				Replies  int    `json:"reply_count"`
				Posts    int    `json:"posts_count"`
				Views    int    `json:"views"`
				Likes    int    `json:"like_count"`
				Avatar   string `json:"avatar_template"`
			} `json:"topics"`
		} `json:"topic_list"`
	}
	if json.Unmarshal(raw, &data) != nil || data.TopicList == nil || len(data.TopicList.Topics) > 100 {
		return discovery.Feed{}, discovery.ErrUnavailable
	}
	feed := discovery.Feed{Items: []content.Item{}, Page: page, HasMore: data.TopicList.More != "", SourceTitle: "Linux.do · 最新话题"}
	for _, topic := range data.TopicList.Topics {
		if topic.ID <= 0 {
			continue
		}
		id := strconv.FormatInt(topic.ID, 10)
		date, _ := time.Parse(time.RFC3339Nano, topic.Created)
		published := int64(0)
		if !date.IsZero() {
			published = date.Unix()
		}
		author := strings.TrimSpace(topic.Author)
		if author == "" {
			author = "Linux.do"
		}
		replies := topic.Replies
		if replies == 0 && topic.Posts > 0 {
			replies = topic.Posts - 1
		}
		avatar := discourseAvatar(topic.Avatar)
		item := content.Item{ID: "linuxdo:" + id, Platform: "linuxdo", PlatformID: id, Kind: "article", Title: topic.Title, Author: author, AuthorAvatarURL: avatar, Category: topic.Category, ReplyCount: replies, ViewCount: int64(topic.Views), LikeCount: topic.Likes, URL: "https://linux.do/t/" + id, Description: feeds.Text(topic.Excerpt), PreviewText: feeds.Text(topic.Excerpt), PublishedAt: published}
		if content.Validate(item) == nil {
			feed.Items = append(feed.Items, item)
		}
	}
	feed.Total = len(feed.Items)
	return feed, nil
}

func discourseAvatar(raw string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "{size}", "64"))
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if strings.HasPrefix(raw, "/") {
		return "https://linux.do" + raw
	}
	if strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "http://") {
		return raw
	}
	return ""
}
