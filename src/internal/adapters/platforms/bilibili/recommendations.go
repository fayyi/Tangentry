package bilibili

import (
	"context"
	"encoding/json"
	"html"
	"net/url"
	"strconv"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
)

// Native homepage recommendations, not search or a creator's uploads.
func (c *Client) Recommendations(ctx context.Context, pages ...int) (discovery.Feed, error) {
	page := 1
	if len(pages) > 0 {
		page = pages[0]
	}
	if page < 1 || page > 10000 {
		return discovery.Feed{}, discovery.ErrInvalidQuery
	}
	var data struct {
		Items []struct {
			Business  json.RawMessage `json:"business_info"`
			IsAd      bool            `json:"is_ad"`
			BVID      string          `json:"bvid"`
			Goto      string          `json:"goto"`
			Title     string          `json:"title"`
			Picture   string          `json:"pic"`
			Duration  int             `json:"duration"`
			Published int64           `json:"pubdate"`
			Owner     struct {
				Face string `json:"face"`
				MID  int64  `json:"mid"`
				Name string `json:"name"`
			} `json:"owner"`
		} `json:"item"`
	}
	params := url.Values{"fresh_type": {"3"}, "ps": {"20"}, "fresh_idx": {strconv.Itoa(page)}, "fresh_idx_1h": {strconv.Itoa(page)}, "feed_version": {"V8"}}
	if page > 1 {
		params.Set("fresh_type", "4")
	}
	if err := c.get(ctx, "/x/web-interface/index/top/feed/rcmd", params, &data, false); err != nil {
		return discovery.Feed{}, err
	}
	if data.Items == nil || len(data.Items) > 100 {
		return discovery.Feed{}, discovery.ErrUnavailable
	}
	result := discovery.Feed{Items: []content.Item{}, Page: page, SourceTitle: "B站推荐"}
	seen := map[string]bool{}
	for _, v := range data.Items {
		if v.Goto != "av" || v.IsAd || (len(v.Business) > 0 && string(v.Business) != "null") {
			continue
		}
		kind, id, err := discovery.ParseQuery(v.BVID)
		if err != nil || kind != "video" {
			return discovery.Feed{}, discovery.ErrUnavailable
		}
		item := content.Item{ID: "bilibili:" + id, Platform: "bilibili", PlatformID: id, Kind: "video", Title: html.UnescapeString(v.Title), Author: v.Owner.Name, AuthorID: strconv.FormatInt(v.Owner.MID, 10), URL: "https://www.bilibili.com/video/" + id + "/", ThumbnailURL: secureImage(v.Picture), DurationSeconds: v.Duration, PublishedAt: v.Published}
		item.AuthorAvatarURL = secureImage(v.Owner.Face)
		if content.Validate(item) != nil {
			return discovery.Feed{}, discovery.ErrUnavailable
		}
		if !seen[item.ID] {
			seen[item.ID] = true
			result.Items = append(result.Items, item)
		}
	}
	result.Total = len(result.Items)
	result.HasMore = len(data.Items) > 0
	return result, nil
}
