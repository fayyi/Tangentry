package bilibili

import (
	"context"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"tangentry/internal/content"
	"tangentry/internal/discovery"
)

var searchBVPattern = regexp.MustCompile(`^BV[0-9A-Za-z]{10}$`)

type searchResult struct {
	Result []struct {
		BVID     string `json:"bvid"`
		Title    string `json:"title"`
		Author   string `json:"author"`
		MID      int64  `json:"mid"`
		Pic      string `json:"pic"`
		Desc     string `json:"description"`
		Duration string `json:"duration"`
		Pubdate  int64  `json:"pubdate"`
	} `json:"result"`
	PageInfo struct {
		Num   int `json:"num"`
		Size  int `json:"size"`
		Pages int `json:"pages"`
		Total int `json:"total"`
	} `json:"pageinfo"`
}

// Search performs B站's WBI signed video keyword search. The adapter keeps
// pagination opaque to the unified search service (page numbers are encoded in
// the cursor string).
func (c *Client) Search(ctx context.Context, query, cursor string) (discovery.SearchPage, error) {
	page := 1
	if cursor != "" {
		value, err := strconv.Atoi(cursor)
		if err != nil || value < 1 || value > 1000 {
			return discovery.SearchPage{}, discovery.ErrInvalidQuery
		}
		page = value
	}
	key, err := c.mixinKey(ctx)
	if err != nil {
		return discovery.SearchPage{}, err
	}
	params := url.Values{"search_type": {"video"}, "keyword": {query}, "page": {strconv.Itoa(page)}, "pagesize": {"20"}, "order": {"totalrank"}}
	sign(params, key, nowUnix())
	var data searchResult
	if err := c.get(ctx, "/x/web-interface/wbi/search/type", params, &data, false); err != nil {
		return discovery.SearchPage{}, err
	}
	items := make([]content.Item, 0, len(data.Result))
	for _, value := range data.Result {
		if !searchBVPattern.MatchString(value.BVID) {
			continue
		}
		item := content.Item{ID: "bilibili:" + value.BVID, Platform: "bilibili", PlatformID: value.BVID, Kind: "video", Title: html.UnescapeString(stripTags(value.Title)), Author: html.UnescapeString(value.Author), AuthorID: strconv.FormatInt(value.MID, 10), URL: "https://www.bilibili.com/video/" + value.BVID + "/", ThumbnailURL: secureImage(value.Pic), Description: html.UnescapeString(stripTags(value.Desc)), DurationSeconds: duration(value.Duration), PublishedAt: value.Pubdate}
		if content.Validate(item) == nil {
			items = append(items, item)
		}
	}
	return discovery.SearchPage{Items: items, Cursor: nextPage(page, data.PageInfo.Pages), HasMore: data.PageInfo.Pages > page || len(items) == 20, Platform: "bilibili"}, nil
}

func nextPage(page, pages int) string {
	if pages > page {
		return strconv.Itoa(page + 1)
	}
	return ""
}

func nowUnix() int64 { return time.Now().Unix() }

func stripTags(value string) string {
	for {
		start := strings.IndexByte(value, '<')
		if start < 0 {
			return strings.TrimSpace(value)
		}
		end := strings.IndexByte(value[start:], '>')
		if end < 0 {
			return strings.TrimSpace(value[:start])
		}
		value = value[:start] + value[start+end+1:]
	}
}
