// Package discourse reads Linux.do topic feeds using the owner's site session.
package discourse

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
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

func maxInt(values ...int) int {
	result := 0
	for _, value := range values {
		if value > result {
			result = value
		}
	}
	return result
}

func MissingPostIDs(all []int64, loaded []content.DiscussionPost, limit int) []int64 {
	if limit < 1 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	seen := make(map[int64]struct{}, len(loaded))
	for _, post := range loaded {
		seen[post.ID] = struct{}{}
	}
	result := make([]int64, 0, limit)
	for _, id := range all {
		if id > 0 {
			if _, ok := seen[id]; !ok {
				result = append(result, id)
				if len(result) == limit {
					break
				}
			}
		}
	}
	return result
}

type SessionState struct {
	LoggedIn bool   `json:"loggedIn"`
	Username string `json:"username,omitempty"`
}

const discourseBrowserUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36"

const discourseMaxResponseBody = 4 << 20

var diagnosticSecretPattern = regexp.MustCompile(`(?i)(?:cookie|token|secret|sig(?:nature)?|key|auth(?:orization)?)\s*[:=]\s*["']?[^\s,;"']+|[A-Za-z0-9_\-]{32,}`)

func sanitizeDiagnosticSample(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if len(raw) > 200 {
		raw = raw[:200]
	}
	return diagnosticSecretPattern.ReplaceAllString(raw, "<已脱敏>")
}

func sanitizeDiagnosticURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "<无效 URL>"
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func pageResponseMessage(path string, response platformfetch.Response, reason string) string {
	parts := []string{"Linux.do 页面请求失败", "路径=" + path}
	if reason != "" {
		parts = append(parts, reason)
	}
	parts = append(parts, "状态码="+strconv.Itoa(response.Status))
	if response.ContentType != "" {
		parts = append(parts, "content-type="+response.ContentType)
	}
	if response.URL != "" {
		parts = append(parts, "最终 URL="+sanitizeDiagnosticURL(response.URL))
	}
	if sample := sanitizeDiagnosticSample(response.Body); sample != "" {
		parts = append(parts, "响应片段="+sample)
	}
	if response.Restricted || looksLikeCloudflareResponse(response.Status, response.ContentType, []byte(response.Body)) {
		parts = append(parts, "疑似 Cloudflare 验证页")
	}
	return strings.Join(parts, "；") + "。"
}

func bridgeFailure(path string, bridgeErr error) error {
	if bridgeErr == nil {
		return nil
	}
	return &discovery.PlatformError{Message: "Linux.do 页面请求桥不可用（路径=" + path + "）：" + sanitizeDiagnosticSample(bridgeErr.Error()) + "。"}
}

func invalidJSONFailure(path string, response platformfetch.Response) error {
	return &discovery.PlatformError{Message: pageResponseMessage(path, response, "响应不是 JSON")}
}

func malformedDetailFailure(path string, responseBody []byte, cause error) error {
	response := platformfetch.Response{Status: http.StatusOK, Body: string(responseBody), URL: "https://linux.do" + path, ContentType: "application/json"}
	reason := "详情 JSON 结构无法解析"
	if cause != nil {
		reason += "（" + sanitizeDiagnosticSample(cause.Error()) + "）"
	}
	return &discovery.PlatformError{Message: pageResponseMessage(path, response, reason)}
}

func optionalNotFound(path string) bool {
	if path == "/session/current.json" {
		return true
	}
	for _, name := range []string{"latest", "hot", "top", "new", "unread"} {
		if path == "/"+name+".json" || strings.HasPrefix(path, "/"+name+".json?") {
			return true
		}
	}
	return false
}

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
	var bridgeErr error
	if c.Page != nil {
		response, err := c.Page.Fetch(ctx, "linuxdo", path, platformfetch.Request{Method: http.MethodGet, Headers: map[string]string{
			"Accept":           "application/json, text/javascript, */*; q=0.01",
			"X-Requested-With": "XMLHttpRequest",
		}})
		if err == nil {
			raw := []byte(response.Body)
			if response.Restricted || response.Status == http.StatusUnauthorized || response.Status == http.StatusTooManyRequests || response.Status == http.StatusForbidden && response.ContentType == "" || response.Status == http.StatusForbidden && !json.Valid(raw) && !strings.Contains(strings.ToLower(response.ContentType), "json") || looksLikeCloudflareResponse(response.Status, response.ContentType, raw) {
				return nil, response.Status, &discovery.PlatformError{Message: pageResponseMessage(path, response, "需要在应用内完成 Linux.do 验证"), Restricted: true}
			}
			if response.Status != http.StatusOK {
				if response.Status == http.StatusNotFound && optionalNotFound(path) {
					return nil, response.Status, nil
				}
				return nil, response.Status, &discovery.PlatformError{Message: pageResponseMessage(path, response, "HTTP 请求未成功")}
			}
			if !json.Valid(raw) {
				return nil, response.Status, invalidJSONFailure(path, response)
			}
			return raw, response.Status, nil
		}
		bridgeErr = err
		// A native bridge is optional. If it cannot create or use a WebView,
		// retain the server-side HTTP fallback below.
	}
	if c.HTTP == nil {
		return nil, 0, bridgeFailure(path, bridgeErr)
	}
	origin := "https://linux.do" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin, nil)
	if err != nil {
		return nil, 0, &discovery.PlatformError{Message: "Linux.do 页面请求地址无效（路径=" + path + "）"}
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
		message := "Linux.do 页面请求失败（路径=" + path + "）：" + sanitizeDiagnosticSample(err.Error())
		if bridgeErr != nil {
			message += "；页面请求桥原因=" + sanitizeDiagnosticSample(bridgeErr.Error())
		}
		return nil, 0, &discovery.PlatformError{Message: message + "。"}
	}
	defer res.Body.Close()
	responseURL := req.URL.String()
	if res.Request != nil && res.Request.URL != nil {
		responseURL = res.Request.URL.String()
	}
	raw, readErr := io.ReadAll(io.LimitReader(res.Body, discourseMaxResponseBody+1))
	response := platformfetch.Response{Status: res.StatusCode, Body: string(raw), URL: responseURL, ContentType: res.Header.Get("Content-Type")}
	if readErr != nil {
		return nil, res.StatusCode, &discovery.PlatformError{Message: pageResponseMessage(path, response, "读取响应失败")}
	}
	if strings.EqualFold(res.Header.Get("cf-mitigated"), "challenge") {
		return nil, res.StatusCode, &discovery.PlatformError{Message: pageResponseMessage(path, response, "需要在应用内完成 Linux.do 验证；若使用 Cookie，请确认包含 cf_clearance"), Restricted: true}
	}
	switch res.StatusCode {
	case 401:
		return nil, res.StatusCode, &discovery.PlatformError{Message: pageResponseMessage(path, response, "Linux.do 登录已失效，请在应用内完成验证后重试"), Restricted: true}
	case 403:
		if response.ContentType == "" || !json.Valid(raw) && !strings.Contains(strings.ToLower(response.ContentType), "json") {
			return nil, res.StatusCode, &discovery.PlatformError{Message: pageResponseMessage(path, response, "需要在应用内完成 Linux.do 验证；若使用 Cookie，请确认包含 cf_clearance"), Restricted: true}
		}
		return nil, res.StatusCode, &discovery.PlatformError{Message: pageResponseMessage(path, response, "HTTP 请求未成功")}
	case 429:
		return nil, res.StatusCode, &discovery.PlatformError{Message: pageResponseMessage(path, response, "Linux.do 暂时限制请求，请稍后在应用内重试"), Restricted: true}
	}
	if len(raw) > discourseMaxResponseBody {
		return nil, res.StatusCode, &discovery.PlatformError{Message: pageResponseMessage(path, response, "响应超过 4 MiB 大小限制")}
	}
	if res.StatusCode != http.StatusOK {
		return nil, res.StatusCode, &discovery.PlatformError{Message: pageResponseMessage(path, response, "HTTP 请求未成功")}
	}
	if !json.Valid(raw) {
		if looksLikeCloudflareResponse(response.Status, response.ContentType, raw) {
			return nil, res.StatusCode, &discovery.PlatformError{Message: pageResponseMessage(path, response, "需要在应用内完成 Linux.do 验证"), Restricted: true}
		}
		return nil, res.StatusCode, invalidJSONFailure(path, response)
	}
	if save != nil && req.Header.Get("Cookie") != "" {
		if err := save(res.Cookies()); err != nil {
			return nil, res.StatusCode, &discovery.PlatformError{Message: "Linux.do 登录态保存失败（路径=" + path + "），请重试。"}
		}
	}
	return raw, res.StatusCode, nil
}

func looksLikeCloudflareResponse(status int, contentType string, raw []byte) bool {
	if json.Valid(raw) || strings.Contains(strings.ToLower(contentType), "json") {
		return false
	}
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if contentType != "" && !strings.Contains(contentType, "text/html") {
		return false
	}
	value := strings.ToLower(string(raw))
	if contentType == "" && status != http.StatusForbidden && strings.TrimSpace(value) == "" {
		return false
	}
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
	data, err := discourseObject(raw)
	if err != nil {
		return content.Item{}, malformedDetailFailure("/t/"+id+".json", raw, err)
	}
	responseID := discourseInt64(data["id"])
	if responseID != number {
		return content.Item{}, &discovery.PlatformError{Message: "Linux.do 话题详情与请求 ID 不一致（请求=" + id + "，响应=" + strconv.FormatInt(responseID, 10) + "）。"}
	}
	title := discourseString(data["title"])
	if strings.TrimSpace(title) == "" {
		title = "Linux.do 话题 " + id
	}
	date, _ := time.Parse(time.RFC3339Nano, discourseString(data["created_at"]))
	item := content.Item{
		ID: "linuxdo:" + id, Platform: "linuxdo", PlatformID: id, Kind: "article",
		Title: title, Category: discourseString(data["category_name"]),
		ViewCount: maxInt64(discourseInt64(data["views"]), 0), LikeCount: discourseCount(data["like_count"]),
		URL: "https://linux.do/t/" + id,
	}
	if !date.IsZero() {
		item.PublishedAt = date.Unix()
	}
	postStream, _ := discourseObject(data["post_stream"])
	rawPosts := discourseArray(postStream["posts"])
	posts := make([]content.DiscussionPost, 0, len(rawPosts))
	for _, rawPost := range rawPosts {
		post, ok := decodeDiscoursePost(rawPost)
		if !ok {
			continue
		}
		cookedBlocks := feeds.ParseArticleBlocks(post.Body, item.URL)
		if len(cookedBlocks) == 0 {
			continue
		}
		if post.Number <= 0 {
			continue
		}
		if post.Number == 1 {
			item.Author = firstNonEmpty(post.Username, post.Name)
			item.AuthorAvatarURL = discourseAvatar(post.Avatar)
		}
		created, _ := time.Parse(time.RFC3339Nano, post.Created)
		if post.ID <= 0 {
			post.ID = int64(post.Number)
		}
		username := firstNonEmpty(post.Username, post.Name, "Linux.do")
		posts = append(posts, content.DiscussionPost{ID: post.ID, Number: post.Number, Username: username, Name: post.Name, AvatarURL: discourseAvatar(post.Avatar), CreatedAt: created.Unix(), LikeCount: post.Likes, ReplyToNumber: post.ReplyTo, ReplyToUser: post.ReplyUser, ReplyToPostID: post.ReplyID, Blocks: cookedBlocks})
	}
	var mainText []string
	if len(posts) > 0 {
		for _, block := range posts[0].Blocks {
			if block.Kind != "image" && block.Kind != "emoji" && block.Text != "" {
				mainText = append(mainText, block.Text)
			}
		}
	}
	item.Description = feeds.Text(strings.Join(mainText, "\n\n"))
	replyCount := discourseCount(data["reply_count"])
	postsCount := discourseCount(data["posts_count"])
	if replyCount > 0 {
		item.ReplyCount = replyCount
	} else if len(rawPosts) > 1 {
		item.ReplyCount = len(rawPosts) - 1
	}
	if len(posts) > 0 {
		item.ArticleBlocks = posts[0].Blocks
		participants := map[string]struct{}{}
		for _, post := range posts {
			participants[post.Username] = struct{}{}
		}
		postIDs := discourseIDs(postStream["stream"])
		if len(postIDs) == 0 {
			for _, post := range posts {
				postIDs = append(postIDs, post.ID)
			}
		}
		item.Discussion = &content.Discussion{TopicID: id, TotalPosts: maxInt(postsCount, len(postIDs), replyCount+1), LoadedPosts: len(posts), Participating: len(participants), PostIDs: postIDs, Posts: posts}
	}
	if content.Validate(item) != nil {
		return content.Item{}, &discovery.PlatformError{Message: "Linux.do 话题详情缺少可显示的正文或字段，无法渲染。"}
	}
	return item, nil
}

func (c *Client) TopicPosts(ctx context.Context, id string, postIDs []int64) (content.Discussion, error) {
	if _, err := strconv.ParseInt(id, 10, 64); err != nil || len(postIDs) == 0 || len(postIDs) > 50 {
		return content.Discussion{}, discovery.ErrInvalidQuery
	}
	query := make([]string, 0, len(postIDs))
	for _, postID := range postIDs {
		if postID <= 0 {
			return content.Discussion{}, discovery.ErrInvalidQuery
		}
		query = append(query, "post_ids[]="+strconv.FormatInt(postID, 10))
	}
	raw, _, err := c.read(ctx, "/t/"+id+"/posts.json?"+strings.Join(query, "&"))
	if err != nil {
		return content.Discussion{}, err
	}
	data, err := discourseObject(raw)
	if err != nil {
		return content.Discussion{}, malformedDetailFailure("/t/"+id+"/posts.json", raw, err)
	}
	postStream, _ := discourseObject(data["post_stream"])
	rawPosts := discourseArray(postStream["posts"])
	posts := make([]content.DiscussionPost, 0, len(rawPosts))
	for _, rawPost := range rawPosts {
		post, ok := decodeDiscoursePost(rawPost)
		if !ok {
			continue
		}
		blocks := feeds.ParseArticleBlocks(post.Body, "https://linux.do/t/"+id)
		if len(blocks) == 0 {
			continue
		}
		if post.Number <= 0 {
			continue
		}
		created, _ := time.Parse(time.RFC3339Nano, post.Created)
		if post.ID <= 0 {
			post.ID = int64(post.Number)
		}
		username := firstNonEmpty(post.Username, post.Name, "Linux.do")
		posts = append(posts, content.DiscussionPost{ID: post.ID, Number: post.Number, Username: username, Name: post.Name, AvatarURL: discourseAvatar(post.Avatar), CreatedAt: created.Unix(), LikeCount: post.Likes, ReplyToNumber: post.ReplyTo, ReplyToUser: post.ReplyUser, ReplyToPostID: post.ReplyID, Blocks: blocks})
	}
	if len(posts) == 0 {
		return content.Discussion{}, &discovery.PlatformError{Message: "Linux.do 楼层接口未返回请求的楼层正文。"}
	}
	return content.Discussion{TopicID: id, LoadedPosts: len(posts), Posts: posts}, nil
}

type discoursePost struct {
	ID        int64
	Number    int
	Body      string
	Username  string
	Name      string
	Avatar    string
	Created   string
	Likes     int
	ReplyTo   int
	ReplyUser string
	ReplyID   int64
}

func discourseObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, errors.New("expected JSON object")
	}
	return fields, nil
}

func discourseArray(raw json.RawMessage) []json.RawMessage {
	var values []json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &values) != nil {
		return nil
	}
	return values
}

func discourseString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed != "" && (trimmed[0] == '-' || trimmed[0] >= '0' && trimmed[0] <= '9') {
		if json.Valid(raw) {
			return trimmed
		}
	}
	return ""
}

func discourseInt64(raw json.RawMessage) int64 {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return 0
	}
	if value[0] == '"' {
		var stringValue string
		if json.Unmarshal(raw, &stringValue) != nil {
			return 0
		}
		value = strings.TrimSpace(stringValue)
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err == nil {
		return parsed
	}
	floatValue, err := strconv.ParseFloat(value, 64)
	const maxInt64Value = float64(^uint64(0) >> 1)
	const minInt64Value = -maxInt64Value - 1
	if err != nil || floatValue > maxInt64Value || floatValue < minInt64Value {
		return 0
	}
	return int64(floatValue)
}

func discourseCount(raw json.RawMessage) int {
	value := discourseInt64(raw)
	if value <= 0 {
		return 0
	}
	max := int64(^uint(0) >> 1)
	if value > max {
		return int(max)
	}
	return int(value)
}

func discourseIDs(raw json.RawMessage) []int64 {
	values := discourseArray(raw)
	ids := make([]int64, 0, len(values))
	for _, value := range values {
		if id := discourseInt64(value); id > 0 {
			ids = append(ids, id)
		}
	}
	return ids
}

func decodeDiscoursePost(raw json.RawMessage) (discoursePost, bool) {
	fields, err := discourseObject(raw)
	if err != nil {
		return discoursePost{}, false
	}
	return discoursePost{
		ID:        discourseInt64(fields["id"]),
		Number:    discourseCount(fields["post_number"]),
		Body:      discourseString(fields["cooked"]),
		Username:  discourseString(fields["username"]),
		Name:      discourseString(fields["name"]),
		Avatar:    discourseString(fields["avatar_template"]),
		Created:   discourseString(fields["created_at"]),
		Likes:     discourseCount(fields["like_count"]),
		ReplyTo:   discourseCount(fields["reply_to_post_number"]),
		ReplyUser: discourseReplyUser(fields["reply_to_user"]),
		ReplyID:   discourseInt64(fields["reply_to_post_id"]),
	}, true
}

func discourseReplyUser(raw json.RawMessage) string {
	if value := discourseString(raw); value != "" {
		return value
	}
	fields, err := discourseObject(raw)
	if err != nil {
		return ""
	}
	return firstNonEmpty(discourseString(fields["username"]), discourseString(fields["name"]))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func maxInt64(values ...int64) int64 {
	var result int64
	for _, value := range values {
		if value > result {
			result = value
		}
	}
	return result
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
