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
)

type Client struct {
	HTTP    *http.Client
	Cookie  func(string) string
	Session func() (string, func([]*http.Cookie) error)
	UserKey func() (string, string)
}

type SessionState struct {
	LoggedIn bool   `json:"loggedIn"`
	Username string `json:"username,omitempty"`
}

func (c *Client) read(ctx context.Context, path string) ([]byte, int, error) {
	if c.HTTP == nil {
		return nil, 0, discovery.ErrUnavailable
	}
	origin := "https://linux.do" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin, nil)
	if err != nil {
		return nil, 0, discovery.ErrUnavailable
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Tangentry/0.1")
	var save func([]*http.Cookie) error
	if c.Session != nil {
		header, commit := c.Session()
		req.Header.Set("Cookie", header)
		save = commit
	} else if c.Cookie != nil {
		req.Header.Set("Cookie", c.Cookie(origin))
	}
	if c.UserKey != nil {
		key, clientID := c.UserKey()
		if key != "" {
			req.Header.Del("Cookie")
			req.Header.Set("User-Api-Key", key)
			req.Header.Set("User-Api-Client-Id", clientID)
			save = nil
		}
	}
	client := *c.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		return nil, 0, discovery.ErrUnavailable
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case 401:
		return nil, res.StatusCode, &discovery.PlatformError{Message: "Linux.do 登录已失效，请重新保存原站登录信息。", Restricted: true}
	case 403:
		return nil, res.StatusCode, &discovery.PlatformError{Message: "Linux.do 拒绝访问，请在原站完成验证后重试；已保存话题仍可浏览。", Restricted: true}
	case 429:
		return nil, res.StatusCode, &discovery.PlatformError{Message: "Linux.do 暂时限制请求，请稍后手动重试。", Restricted: true}
	}
	if res.StatusCode != http.StatusOK {
		return nil, res.StatusCode, discovery.ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if err != nil || len(raw) > 2<<20 || !json.Valid(raw) {
		return nil, res.StatusCode, discovery.ErrUnavailable
	}
	if save != nil && req.Header.Get("Cookie") != "" {
		if err := save(res.Cookies()); err != nil {
			return nil, res.StatusCode, discovery.ErrUnavailable
		}
	}
	return raw, res.StatusCode, nil
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
		ID      int64  `json:"id"`
		Title   string `json:"title"`
		Created string `json:"created_at"`
		Stream  struct {
			Posts []struct {
				Number   int    `json:"post_number"`
				Body     string `json:"cooked"`
				Username string `json:"username"`
			} `json:"posts"`
		} `json:"post_stream"`
	}
	if json.Unmarshal(raw, &data) != nil || data.ID != number {
		return content.Item{}, discovery.ErrUnavailable
	}
	date, _ := time.Parse(time.RFC3339Nano, data.Created)
	item := content.Item{ID: "linuxdo:" + id, Platform: "linuxdo", PlatformID: id, Kind: "article", Title: data.Title, URL: "https://linux.do/t/" + id}
	if !date.IsZero() {
		item.PublishedAt = date.Unix()
	}
	for _, post := range data.Stream.Posts {
		if post.Number == 1 {
			item.Description = feeds.Text(post.Body)
			item.Author = post.Username
			break
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
	feed, status, err := c.readTopics(ctx, "hot", page)
	if status == 404 {
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
	if filter == "hot" {
		feed.SourceTitle = "Linux.do · 热门话题"
	}
	return feed, status, err
}

func Parse(raw []byte, page int) (discovery.Feed, error) {
	var data struct {
		TopicList *struct {
			More   string `json:"more_topics_url"`
			Topics []struct {
				ID      int64  `json:"id"`
				Title   string `json:"title"`
				Created string `json:"created_at"`
				Excerpt string `json:"excerpt"`
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
		item := content.Item{ID: "linuxdo:" + id, Platform: "linuxdo", PlatformID: id, Kind: "article", Title: topic.Title, Author: "Linux.do", URL: "https://linux.do/t/" + id, PublishedAt: published}
		if content.Validate(item) == nil {
			feed.Items = append(feed.Items, item)
		}
	}
	feed.Total = len(feed.Items)
	return feed, nil
}
