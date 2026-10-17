// Package bilibili translates Bilibili's public API into Tangentry content.
package bilibili

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"tangentry/internal/playback"
)

const pageSize = 24
const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"

type Client struct {
	http              *http.Client
	baseURL           string
	cookie            string
	cookieProvider    func(string) string
	userAgentProvider func(string) string
	mu                sync.Mutex
	key               string
	keyExpires        time.Time
	restrictedUntil   time.Time
	videoFallback     VideoExtractor
	playbackFallback  PlaybackExtractor
	refresher         *CredentialRefresher
}

// A generic media extractor is optional; platform failures remain errors if it also fails.
type VideoExtractor interface {
	Video(context.Context, string) (content.Item, error)
}

// PlaybackExtractor is an optional infrastructure fallback. It resolves one
// browser-playable HTTPS representation; it does not download or proxy media.
type PlaybackExtractor interface {
	Playback(context.Context, string) (playback.Source, error)
}

func NewClient(client *http.Client, cookie string, fallback ...VideoExtractor) *Client {
	c := &Client{http: client, baseURL: "https://api.bilibili.com", cookie: cookie}
	if len(fallback) > 0 {
		c.videoFallback = fallback[0]
		if resolver, ok := fallback[0].(PlaybackExtractor); ok {
			c.playbackFallback = resolver
		}
	}
	return c
}

// SetCookieProvider is wired once before serving requests.
func (c *Client) SetCookieProvider(provider func(string) string) { c.cookieProvider = provider }

func (c *Client) SetUserAgentProvider(provider func(string) string) { c.userAgentProvider = provider }

// SetCredentialRefresher enables best-effort, throttled Cookie rotation.
func (c *Client) SetCredentialRefresher(refresher *CredentialRefresher) { c.refresher = refresher }

func (c *Client) CreatorVideos(ctx context.Context, uid string, page int) (discovery.Feed, error) {
	key, err := c.mixinKey(ctx)
	if err != nil {
		return discovery.Feed{}, err
	}
	params := url.Values{"mid": {uid}, "pn": {strconv.Itoa(page)}, "ps": {strconv.Itoa(pageSize)}, "order": {"pubdate"}, "platform": {"web"}, "web_location": {"1550101"}}
	sign(params, key, time.Now().Unix())
	var data struct {
		List *struct {
			Videos []struct {
				BVID        string `json:"bvid"`
				Title       string `json:"title"`
				Author      string `json:"author"`
				MID         int64  `json:"mid"`
				Picture     string `json:"pic"`
				Description string `json:"description"`
				Duration    string `json:"length"`
				Created     int64  `json:"created"`
			} `json:"vlist"`
		} `json:"list"`
		Page *struct {
			Count  int `json:"count"`
			Number int `json:"pn"`
			Size   int `json:"ps"`
		} `json:"page"`
		IsRisk   bool `json:"is_risk"`
		GaiaType int  `json:"gaia_res_type"`
	}
	if err := c.get(ctx, "/x/space/wbi/arc/search", params, &data, false); err != nil {
		return discovery.Feed{}, err
	}
	if data.IsRisk || data.GaiaType != 0 {
		return discovery.Feed{}, c.restricted()
	}
	if data.List == nil || data.List.Videos == nil || data.Page == nil || data.Page.Count < 0 || (data.Page.Number > 0 && data.Page.Number != page) {
		return discovery.Feed{}, &discovery.PlatformError{Message: "B站返回了不完整的投稿列表，请稍后重试。"}
	}
	feed := discovery.Feed{Items: make([]content.Item, 0, len(data.List.Videos)), Page: page, Total: data.Page.Count, SourceTitle: "UP 主 " + uid, Query: uid}
	for _, video := range data.List.Videos {
		authorID := uid
		if video.MID > 0 {
			authorID = strconv.FormatInt(video.MID, 10)
		}
		item := content.Item{ID: "bilibili:" + video.BVID, Platform: "bilibili", PlatformID: video.BVID, Kind: "video", Title: html.UnescapeString(video.Title), Author: video.Author, AuthorID: authorID, URL: "https://www.bilibili.com/video/" + video.BVID + "/", ThumbnailURL: secureImage(video.Picture), Description: video.Description, DurationSeconds: duration(video.Duration), PublishedAt: video.Created}
		if err := content.Validate(item); err != nil {
			return discovery.Feed{}, &discovery.PlatformError{Message: "B站返回了不完整的视频数据，请稍后重试。"}
		}
		feed.Items = append(feed.Items, item)
	}
	if len(feed.Items) > 0 && feed.Items[0].AuthorID == uid {
		feed.SourceTitle = feed.Items[0].Author
	}
	size := pageSize
	if data.Page.Size > 0 {
		size = data.Page.Size
	}
	feed.HasMore = page*size < feed.Total && len(feed.Items) > 0
	return feed, nil
}

func (c *Client) Video(ctx context.Context, bvid string) (content.Item, error) {
	var data struct {
		BVID        string `json:"bvid"`
		Title       string `json:"title"`
		Picture     string `json:"pic"`
		Description string `json:"desc"`
		Duration    int    `json:"duration"`
		Created     int64  `json:"pubdate"`
		Owner       struct {
			Face string `json:"face"`
			Name string `json:"name"`
			MID  int64  `json:"mid"`
		} `json:"owner"`
	}
	if err := c.get(ctx, "/x/web-interface/view", url.Values{"bvid": {bvid}}, &data, false); err != nil {
		return c.fallbackVideo(ctx, bvid, err)
	}
	item := content.Item{ID: "bilibili:" + data.BVID, Platform: "bilibili", PlatformID: data.BVID, Kind: "video", Title: html.UnescapeString(data.Title), Author: data.Owner.Name, AuthorID: strconv.FormatInt(data.Owner.MID, 10), URL: "https://www.bilibili.com/video/" + data.BVID + "/", ThumbnailURL: secureImage(data.Picture), Description: data.Description, DurationSeconds: data.Duration, PublishedAt: data.Created}
	item.AuthorAvatarURL = secureImage(data.Owner.Face)
	if err := content.Validate(item); err != nil || data.BVID != bvid {
		return c.fallbackVideo(ctx, bvid, &discovery.PlatformError{Message: "B站返回了不完整或不匹配的视频资料，请重新获取。"})
	}
	return item, nil
}

func (c *Client) fallbackVideo(ctx context.Context, bvid string, original error) (content.Item, error) {
	if ctx.Err() != nil {
		return content.Item{}, ctx.Err()
	}
	if c.videoFallback != nil && errors.Is(original, discovery.ErrUnavailable) {
		item, err := c.videoFallback.Video(ctx, bvid)
		if ctx.Err() != nil {
			return content.Item{}, ctx.Err()
		}
		if err == nil && item.ID == "bilibili:"+bvid && item.PlatformID == bvid && item.Platform == "bilibili" && item.Kind == "video" && content.Validate(item) == nil {
			return item, nil
		}
	}
	return content.Item{}, original
}

func secureImage(raw string) string {
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if strings.HasPrefix(raw, "http://") {
		return "https://" + strings.TrimPrefix(raw, "http://")
	}
	return raw
}

func duration(raw string) int {
	seconds := 0
	for _, part := range strings.Split(raw, ":") {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return 0
		}
		seconds = seconds*60 + n
	}
	return seconds
}
