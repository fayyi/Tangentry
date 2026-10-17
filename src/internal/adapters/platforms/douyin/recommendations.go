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
)

// This is the website's feed, not an Open Platform API. Rejections are final:
// no signature emulation, credential forwarding, or automatic fallback retries.
func (c *Client) Recommendations(ctx context.Context, page int) (discovery.Feed, error) {
	if page < 1 || page > 10000 {
		return discovery.Feed{}, discovery.ErrInvalidQuery
	}
	if c.HTTP == nil {
		return discovery.Feed{}, discovery.ErrUnavailable
	}
	const endpoint = "https://www.douyin.com/aweme/v2/web/module/feed/"
	cookie := ""
	if c.Cookie != nil {
		cookie = c.Cookie(endpoint)
	}
	if cookie == "" {
		return discovery.Feed{}, &discovery.PlatformError{Message: "抖音网页推荐需要登录信息，请在设置中保存 Cookie。"}
	}
	query := url.Values{"device_platform": {"webapp"}, "aid": {"6383"}, "channel": {"channel_pc_web"}, "module_id": {"3003101"}, "count": {"20"}, "refresh_index": {strconv.Itoa(page)}, "refer_type": {"10"}, "pull_type": {"0"}, "use_lite_type": {"2"}}
	if page > 1 {
		query.Set("pull_type", "2")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+query.Encode(), strings.NewReader("encoded_pre_item_ids="))
	if err != nil {
		return discovery.Feed{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://www.douyin.com/")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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
		return discovery.Feed{}, denied
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, (4<<20)+1))
	if err != nil || len(body) > 4<<20 {
		return discovery.Feed{}, denied
	}
	return decodeRecommendations(body, page, denied)
}

type feedVideo struct {
	ID          string `json:"aweme_id"`
	Description string `json:"desc"`
	Created     int64  `json:"create_time"`
	Author      struct {
		ID   string `json:"uid"`
		Name string `json:"nickname"`
	} `json:"author"`
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
	}
	if json.Unmarshal(body, &response) != nil || response.Status == nil || *response.Status != 0 || response.Videos == nil || len(response.Videos) > 500 {
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
		item := content.Item{ID: "douyin:" + video.ID, Platform: "douyin", PlatformID: video.ID, Kind: "video", Title: title, Description: video.Description, Author: video.Author.Name, AuthorID: video.Author.ID, URL: ref.URL, DurationSeconds: video.Video.Duration / 1000, PublishedAt: video.Created}
		for _, raw := range video.Video.Cover.URLs {
			u, err := url.Parse(raw)
			if err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil {
				item.ThumbnailURL = raw
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
