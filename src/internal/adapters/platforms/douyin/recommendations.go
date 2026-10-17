package douyin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"tangentry/internal/platformfetch"
)

// The desktop bridge executes this request in douyin.com's page context, so
// the site's own security SDK can sign it. Headless/server builds use the
// explicit Cookie fallback below; signatures are never emulated.
func (c *Client) Recommendations(ctx context.Context, page int) (discovery.Feed, error) {
	if page < 1 || page > 10000 {
		return discovery.Feed{}, discovery.ErrInvalidQuery
	}
	if c.HTTP == nil {
		if c.Page == nil {
			return discovery.Feed{}, discovery.ErrUnavailable
		}
	}
	const endpoint = "https://www.douyin.com/aweme/v2/web/module/feed/"
	query := url.Values{"device_platform": {"webapp"}, "aid": {"6383"}, "channel": {"channel_pc_web"}, "module_id": {"3003101"}, "count": {"20"}, "refresh_index": {strconv.Itoa(page)}, "refer_type": {"10"}, "pull_type": {"0"}, "use_lite_type": {"2"}}
	query.Set("update_version_code", "170400")
	query.Set("pc_client_type", "1")
	query.Set("browser_language", "zh-CN")
	query.Set("browser_platform", "MacIntel")
	query.Set("browser_name", "Chrome")
	query.Set("browser_version", "136.0.0.0")
	query.Set("engine_name", "Blink")
	query.Set("engine_version", "136.0.0.0")
	query.Set("os_name", "Mac OS")
	query.Set("os_version", "10.15.7")
	query.Set("platform", "PC")
	if page > 1 {
		query.Set("pull_type", "2")
	}
	if c.Page != nil {
		response, err := c.Page.Fetch(ctx, "douyin", "/aweme/v2/web/module/feed/?"+query.Encode(), platformfetch.Request{
			Method: http.MethodPost,
			Headers: map[string]string{
				"Referer":         "https://www.douyin.com/",
				"Content-Type":    "application/x-www-form-urlencoded",
				"Accept":          "application/json, text/plain, */*",
				"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
			},
			Body: "encoded_pre_item_ids=",
		})
		if err == nil {
			denied := &discovery.PlatformError{Restricted: true, Message: "抖音未提供可用的网页推荐，请在应用内完成验证后重试。"}
			if response.Restricted || response.Status != http.StatusOK || looksLikeChallenge([]byte(response.Body)) {
				return discovery.Feed{}, denied
			}
			return decodeRecommendations([]byte(response.Body), page, denied)
		}
		// The bridge is deliberately best-effort. A native WebView may be
		// unavailable during startup; server-side HTTP remains the fallback.
	}
	cookie := ""
	if c.Cookie != nil {
		cookie = c.Cookie(endpoint)
	}
	if cookie == "" {
		return discovery.Feed{}, &discovery.PlatformError{Message: "抖音网页推荐需要登录信息，请在设置中保存 Cookie。"}
	}
	query = url.Values{"device_platform": {"webapp"}, "aid": {"6383"}, "channel": {"channel_pc_web"}, "module_id": {"3003101"}, "count": {"20"}, "refresh_index": {strconv.Itoa(page)}, "refer_type": {"10"}, "pull_type": {"0"}, "use_lite_type": {"2"}}
	query.Set("update_version_code", "170400")
	query.Set("pc_client_type", "1")
	query.Set("browser_language", "zh-CN")
	query.Set("browser_platform", "MacIntel")
	query.Set("browser_name", "Chrome")
	query.Set("browser_version", "136.0.0.0")
	query.Set("engine_name", "Blink")
	query.Set("engine_version", "136.0.0.0")
	query.Set("os_name", "Mac OS")
	query.Set("os_version", "10.15.7")
	query.Set("platform", "PC")
	if token := cookieValue(cookie, "msToken"); token != "" {
		query.Set("msToken", token)
	}
	if page > 1 {
		query.Set("pull_type", "2")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+query.Encode(), strings.NewReader("encoded_pre_item_ids="))
	if err != nil {
		return discovery.Feed{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	if c.UserAgent != nil {
		if userAgent := c.UserAgent(endpoint); userAgent != "" {
			req.Header.Set("User-Agent", userAgent)
		}
	}
	req.Header.Set("Referer", "https://www.douyin.com/")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Origin", "https://www.douyin.com")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Cookie", cookie)
	client := *c.HTTP
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return discovery.Feed{}, ctx.Err()
		}
		return discovery.Feed{}, &discovery.PlatformError{Message: "抖音推荐暂时无法连接，请稍后手动重试。"}
	}
	defer res.Body.Close()
	denied := &discovery.PlatformError{Restricted: true, Message: "抖音未提供可用的网页推荐；Cookie 不保证通过站点限制。可关闭推荐，使用订阅或导入链接。"}
	if res.StatusCode != http.StatusOK {
		if res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusTooManyRequests {
			return discovery.Feed{}, &discovery.PlatformError{Message: "抖音推荐触发网页验证或限流，请在浏览器打开抖音完成验证后重新导入 Cookie。", Restricted: true}
		}
		return discovery.Feed{}, denied
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, (4<<20)+1))
	if err != nil || len(body) > 4<<20 {
		return discovery.Feed{}, denied
	}
	if looksLikeChallenge(body) {
		return discovery.Feed{}, &discovery.PlatformError{Message: "抖音返回了网页验证页面，桌面版暂不能代替 WebView 完成验证；请重新导入浏览器 Cookie。", Restricted: true}
	}
	return decodeRecommendations(body, page, denied)
}

func cookieValue(header, name string) string {
	for _, pair := range strings.Split(header, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if ok && key == name && len(value) <= 4096 {
			return value
		}
	}
	return ""
}

func looksLikeChallenge(body []byte) bool {
	value := strings.ToLower(string(body))
	return strings.Contains(value, "verify_center") || strings.Contains(value, "verify_page") || strings.Contains(value, "captcha") || strings.Contains(value, "secsdk") || strings.Contains(value, "byted_acrawler") || strings.Contains(value, "slide_verify") || strings.Contains(value, "just a moment") || strings.Contains(value, "cf-chl-") || strings.Contains(value, "challenge-platform") || strings.Contains(value, "cloudflare")
}

type feedVideo struct {
	ID          string `json:"aweme_id"`
	Description string `json:"desc"`
	Created     int64  `json:"create_time"`
	Author      struct {
		ID     string `json:"uid"`
		Name   string `json:"nickname"`
		Avatar struct {
			URLs []string `json:"url_list"`
		} `json:"avatar_thumb"`
	} `json:"author"`
	Statistics struct {
		Likes    int `json:"digg_count"`
		Comments int `json:"comment_count"`
		Plays    int `json:"play_count"`
	} `json:"statistics"`
	Video struct {
		Duration int `json:"duration"`
		Cover    struct {
			URLs []string `json:"url_list"`
		} `json:"cover"`
	} `json:"video"`
}

func decodeRecommendations(body []byte, page int, denied error) (discovery.Feed, error) {
	var response struct {
		Status *int        `json:"status_code"`
		More   int         `json:"has_more"`
		Videos []feedVideo `json:"aweme_list"`
		Data   *struct {
			Status *int        `json:"status_code"`
			More   int         `json:"has_more"`
			Videos []feedVideo `json:"aweme_list"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &response) != nil {
		return discovery.Feed{}, denied
	}
	if response.Data != nil && response.Status == nil {
		response.Status, response.Videos, response.More = response.Data.Status, response.Data.Videos, response.Data.More
	}
	if response.Status == nil || *response.Status != 0 || response.Videos == nil || len(response.Videos) > 500 {
		return discovery.Feed{}, denied
	}
	feed := discovery.Feed{Items: []content.Item{}, Page: page, HasMore: response.More != 0, SourceTitle: "抖音网页推荐"}
	seen := map[string]bool{}
	for _, video := range response.Videos {
		ref, err := discovery.ParseDouyin("https://www.douyin.com/video/" + video.ID)
		if err != nil || ref.ID != video.ID || seen[video.ID] {
			continue
		}
		title := strings.TrimSpace(video.Description)
		if title == "" {
			title = video.Author.Name + "的视频"
		}
		item := content.Item{ID: "douyin:" + video.ID, Platform: "douyin", PlatformID: video.ID, Kind: "video", Title: title, Description: video.Description, Author: video.Author.Name, AuthorID: video.Author.ID, URL: ref.URL, DurationSeconds: video.Video.Duration / 1000, PublishedAt: video.Created, LikeCount: video.Statistics.Likes}
		for _, raw := range video.Video.Cover.URLs {
			u, err := url.Parse(raw)
			if err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil {
				item.ThumbnailURL = raw
				break
			}
		}
		for _, raw := range video.Author.Avatar.URLs {
			u, err := url.Parse(raw)
			if err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil {
				item.AuthorAvatarURL = raw
				break
			}
		}
		if content.Validate(item) == nil {
			feed.Items = append(feed.Items, item)
			seen[video.ID] = true
		}
	}
	if len(response.Videos) > 0 && len(feed.Items) == 0 {
		return discovery.Feed{}, denied
	}
	feed.Total = len(feed.Items)
	return feed, nil
}
