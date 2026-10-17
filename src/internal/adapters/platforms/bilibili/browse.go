package bilibili

import (
	"context"
	"encoding/json"
	"html"
	"net/url"
	"strconv"
	"strings"
	"time"

	"tangentry/internal/content"
	"tangentry/internal/discovery"
)

type BrowsePage struct {
	Items       []content.Item `json:"items"`
	Rooms       []LiveRoom     `json:"rooms,omitempty"`
	Next        string         `json:"next,omitempty"`
	HasMore     bool           `json:"hasMore"`
	Page        int            `json:"page"`
	SourceTitle string         `json:"sourceTitle"`
	Notice      string         `json:"notice,omitempty"`
}
type LiveRoom struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Author    string `json:"author"`
	Thumbnail string `json:"thumbnailUrl"`
	URL       string `json:"url"`
}
type Profile struct {
	LoggedIn bool   `json:"loggedIn"`
	ID       string `json:"id,omitempty"`
	Name     string `json:"name,omitempty"`
	Avatar   string `json:"avatar,omitempty"`
	Level    int    `json:"level,omitempty"`
}
type browseVideo struct {
	BVID        string `json:"bvid"`
	Title       string `json:"title"`
	Picture     string `json:"pic"`
	Description string `json:"desc"`
	Duration    int    `json:"duration"`
	Published   int64  `json:"pubdate"`
	ViewCount   int64  `json:"view_count"`
	Owner       struct {
		MID  int64  `json:"mid"`
		Name string `json:"name"`
		Face string `json:"face"`
	} `json:"owner"`
}

func (v browseVideo) item() (content.Item, bool) {
	kind, id, err := discovery.ParseQuery(v.BVID)
	if err != nil || kind != "video" || id != v.BVID {
		return content.Item{}, false
	}
	item := content.Item{ID: "bilibili:" + v.BVID, Platform: "bilibili", PlatformID: v.BVID, Kind: "video", Title: html.UnescapeString(v.Title), Author: v.Owner.Name, URL: "https://www.bilibili.com/video/" + v.BVID + "/", ThumbnailURL: secureImage(v.Picture), AuthorAvatarURL: secureImage(v.Owner.Face), Description: v.Description, DurationSeconds: v.Duration, PublishedAt: v.Published, ViewCount: v.ViewCount}
	if v.Owner.MID > 0 {
		item.AuthorID = strconv.FormatInt(v.Owner.MID, 10)
	}
	return item, content.Validate(item) == nil
}
func (c *Client) Profile(ctx context.Context) (Profile, error) {
	var data struct {
		LoggedIn bool   `json:"isLogin"`
		MID      int64  `json:"mid"`
		Name     string `json:"uname"`
		Face     string `json:"face"`
		Level    struct {
			Value int `json:"current_level"`
		} `json:"level_info"`
	}
	if err := c.get(ctx, "/x/web-interface/nav", url.Values{}, &data, true); err != nil {
		return Profile{}, err
	}
	if !data.LoggedIn {
		return Profile{}, nil
	}
	if data.MID <= 0 || strings.TrimSpace(data.Name) == "" {
		return Profile{}, discovery.ErrUnavailable
	}
	return Profile{LoggedIn: true, ID: strconv.FormatInt(data.MID, 10), Name: data.Name, Avatar: browseImage(data.Face), Level: data.Level.Value}, nil
}

func browseImage(raw string) string {
	value := secureImage(raw)
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return ""
	}
	return value
}

func (c *Client) Browse(ctx context.Context, kind string, page int, cursor string, area int) (BrowsePage, error) {
	if page < 1 || page > 1000 || len(cursor) > 256 || strings.ContainsAny(cursor, "\r\n\x00") {
		return BrowsePage{}, discovery.ErrInvalidQuery
	}
	switch kind {
	case "dynamic":
		return c.dynamic(ctx, page, cursor)
	case "live":
		return c.live(ctx, page, area)
	case "popular", "ranking", "watchlater":
	default:
		return BrowsePage{}, discovery.ErrInvalidQuery
	}
	endpoint := "/x/web-interface/popular"
	params := url.Values{"pn": {strconv.Itoa(page)}, "ps": {"24"}}
	result := BrowsePage{Items: []content.Item{}, Page: page, SourceTitle: "B站热门"}
	if kind == "ranking" {
		if page != 1 {
			return BrowsePage{}, discovery.ErrInvalidQuery
		}
		endpoint = "/x/web-interface/ranking/v2"
		params = url.Values{"rid": {"0"}, "type": {"all"}}
		result.SourceTitle = "B站全站排行"
	}
	if kind == "watchlater" {
		if page != 1 {
			return BrowsePage{}, discovery.ErrInvalidQuery
		}
		endpoint = "/x/v2/history/toview"
		params = url.Values{}
		result.SourceTitle = "B站稍后再看"
	}
	var data struct {
		List   []browseVideo `json:"list"`
		NoMore bool          `json:"no_more"`
	}
	var raw json.RawMessage
	if err := c.get(ctx, endpoint, params, &raw, false); err != nil {
		return BrowsePage{}, err
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		if kind != "watchlater" || json.Unmarshal(raw, &data.List) != nil {
			return BrowsePage{}, discovery.ErrUnavailable
		}
	}
	if data.List == nil || len(data.List) > 200 {
		return BrowsePage{}, discovery.ErrUnavailable
	}
	seen := map[string]bool{}
	for _, value := range data.List {
		item, ok := value.item()
		if ok && !seen[item.ID] {
			result.Items = append(result.Items, item)
			seen[item.ID] = true
		}
	}
	result.HasMore = kind == "popular" && !data.NoMore && len(result.Items) > 0
	return result, nil
}

const dynamicFeatures = "itemOpusStyle,listOnlyfans,opusBigCover,onlyfansVote,decorationCard,onlyfansAssetsV2,forwardListHidden,ugcDelete"

var dynamicKindNames = map[string]string{"DYNAMIC_TYPE_DRAW": "图文", "DYNAMIC_TYPE_WORD": "文字", "DYNAMIC_TYPE_ARTICLE": "专栏", "DYNAMIC_TYPE_FORWARD": "转发", "DYNAMIC_TYPE_LIVE_RCMD": "直播", "DYNAMIC_TYPE_PGC": "番剧", "DYNAMIC_TYPE_COURSES_SEASON": "课程", "DYNAMIC_TYPE_MUSIC": "音频", "DYNAMIC_TYPE_COMMON_SQUARE": "装扮/活动"}

func (c *Client) dynamic(ctx context.Context, page int, cursor string) (BrowsePage, error) {
	if page > 1 && cursor == "" {
		return BrowsePage{}, discovery.ErrInvalidQuery
	}
	key, err := c.mixinKey(ctx)
	if err != nil {
		return BrowsePage{}, err
	}
	result := BrowsePage{Items: []content.Item{}, Page: page, SourceTitle: "关注动态"}
	params := url.Values{"timezone_offset": {"-480"}, "type": {"all"}, "platform": {"web"}, "features": {dynamicFeatures}, "web_location": {"333.1365"}}
	if cursor != "" {
		params.Set("offset", cursor)
	}
	sign(params, key, time.Now().Unix())
	var data struct {
		Items   []json.RawMessage `json:"items"`
		Offset  string            `json:"offset"`
		HasMore json.RawMessage   `json:"has_more"`
	}
	if err := c.getFrom(ctx, c.baseURL, "https://t.bilibili.com/", "/x/polymer/web-dynamic/v1/feed/all", params, &data, false); err != nil {
		return BrowsePage{}, err
	}
	if data.Items == nil || len(data.Items) > 100 || len(data.Offset) > 256 {
		return BrowsePage{}, discovery.ErrUnavailable
	}
	seen := map[string]bool{}
	for _, raw := range data.Items {
		item, ok := dynamicItem(raw, 0)
		if ok && !seen[item.ID] {
			result.Items = append(result.Items, item)
			seen[item.ID] = true
		}
	}
	result.HasMore = dynamicHasMore(data.HasMore) && data.Offset != "" && data.Offset != cursor
	result.Next = data.Offset
	if len(result.Items) == 0 {
		result.Notice = dynamicNotice(len(data.Items), result.HasMore)
	}
	return result, nil
}

func dynamicNotice(scanned int, more bool) string {
	if scanned == 0 {
		return "B站没有返回任何关注动态。请确认账号已关注 UP 主；若刚登录，可在设置里重新扫码登录后再试。"
	}
	notice := "暂时没有可显示的关注动态。"
	if more {
		notice += "可点「继续浏览」查看更早的动态。"
	}
	return notice
}

func dynamicHasMore(raw json.RawMessage) bool {
	var value bool
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	var number int
	if json.Unmarshal(raw, &number) == nil {
		return number != 0
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text == "1" || strings.EqualFold(text, "true")
	}
	return false
}

// flexCount accepts the dynamic feed's counters, which arrive as numbers in
// some item types and as display strings such as "3.4万" in others.
type flexCount int64

func (f *flexCount) UnmarshalJSON(raw []byte) error {
	var number float64
	if json.Unmarshal(raw, &number) == nil {
		*f = flexCount(number)
		return nil
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		*f = 0
		return nil
	}
	text = strings.TrimSpace(text)
	scale := 1.0
	switch {
	case strings.HasSuffix(text, "亿"):
		scale, text = 1e8, strings.TrimSuffix(text, "亿")
	case strings.HasSuffix(text, "万"):
		scale, text = 1e4, strings.TrimSuffix(text, "万")
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || value < 0 {
		*f = 0
		return nil
	}
	*f = flexCount(value * scale)
	return nil
}

func dynamicVideo(raw json.RawMessage, depth int) (content.Item, bool) {
	item, ok := dynamicItem(raw, depth)
	return item, ok && item.Kind == "video"
}

// dynamicItem parses one feed entry without binding the whole payload to a
// rigid struct. Bilibili changes number/string representations between entry
// types, so each field is read independently and malformed fields are ignored.
func dynamicItem(raw json.RawMessage, depth int) (content.Item, bool) {
	if depth > 2 {
		return content.Item{}, false
	}
	obj := rawObject(raw)
	if obj == nil {
		return content.Item{}, false
	}
	typ := rawString(obj["type"])
	modules := rawObject(obj["modules"])
	author := rawObject(modules["module_author"])
	if author == nil {
		author = rawObject(obj["author"])
	}
	authorID := rawInt64(author["mid"])
	authorName, authorFace := rawString(author["name"]), secureImage(rawString(author["face"]))
	published := rawInt64(author["pub_ts"])
	if published == 0 {
		published = rawInt64(obj["pub_ts"])
	}
	publishedText := rawString(author["pub_time"])
	dynamic := rawObject(modules["module_dynamic"])
	desc := dynamicText(rawObject(dynamic["desc"]))
	if desc == "" {
		desc = rawString(dynamic["desc"])
	}
	id := rawString(obj["id_str"])
	if id == "" {
		id = rawString(obj["id"])
	}
	baseURL := ""
	major := rawObject(dynamic["major"])
	if major == nil {
		major = map[string]json.RawMessage{}
	}
	if (typ == "8" || typ == "DYNAMIC_TYPE_AV") && rawString(major["bvid"]) == "" {
		if rawString(obj["bvid"]) != "" {
			major = obj
		}
	}
	for _, key := range []string{"archive", "article", "draw", "common", "live_rcmd", "pgc", "courses_season", "music"} {
		if value := rawObject(major[key]); value != nil {
			major = value
			break
		}
	}
	if jump := rawString(major["jump_url"]); jump != "" {
		baseURL = dynamicURL(jump)
	}
	if baseURL == "" {
		baseURL = "https://t.bilibili.com/" + id
	}

	if typ == "1" || typ == "DYNAMIC_TYPE_FORWARD" {
		forwardText := desc
		if original, exists := obj["orig"]; exists && string(original) != "null" && len(original) > 0 {
			if item, ok := dynamicItem(original, depth+1); ok {
				if forwardText != "" {
					if strings.TrimSpace(item.Description) == "" {
						item.Description = item.Title
					}
					item.Description = strings.TrimSpace(forwardText + "\n\n转发内容：" + item.Description)
				}
				if published > 0 {
					item.PublishedAt = published
				}
				if publishedText != "" {
					item.PublishedText = publishedText
				}
				if authorName != "" {
					item.Author, item.AuthorID, item.AuthorAvatarURL = authorName, strconv.FormatInt(authorID, 10), authorFace
				}
				return item, content.Validate(item) == nil
			}
		}
		typ = "DYNAMIC_TYPE_WORD"
	}

	if typ == "8" || typ == "DYNAMIC_TYPE_AV" {
		bvid := rawString(major["bvid"])
		if bvid == "" {
			bvid = rawString(obj["bvid"])
		}
		if bvid == "" {
			if kind, value, err := discovery.ParseQuery(baseURL); err == nil && kind == "video" {
				bvid = value
			}
		}
		if bvid == "" {
			return content.Item{}, false
		}
		stat := rawObject(major["stat"])
		viewCount := flexCountValue(stat["view"])
		if viewCount == 0 {
			viewCount = flexCountValue(stat["play"])
		}
		if viewCount == 0 {
			viewCount = flexCountValue(major["view_count"])
		}
		title := html.UnescapeString(rawString(major["title"]))
		item := content.Item{ID: "bilibili:" + bvid, Platform: "bilibili", PlatformID: bvid, Kind: "video", Title: title, Author: authorName, AuthorID: strconv.FormatInt(authorID, 10), AuthorAvatarURL: authorFace, URL: "https://www.bilibili.com/video/" + bvid + "/", ThumbnailURL: browseImage(rawString(major["cover"])), Description: rawString(major["desc"]), DurationSeconds: duration(rawString(major["duration_text"])), PublishedAt: published, PublishedText: publishedText, ViewCount: viewCount}
		if item.AuthorID == "0" {
			item.AuthorID = ""
		}
		if desc != "" {
			item.Description = strings.TrimSpace(strings.TrimSpace(desc) + "\n\n" + item.Description)
		}
		return item, content.Validate(item) == nil
	}

	itemID := id
	if itemID == "" {
		return content.Item{}, false
	}
	text := desc
	kind := "article"
	var images []content.Image
	title := rawString(major["title"])
	thumbnail := ""
	if typ == "DYNAMIC_TYPE_DRAW" {
		kind = "image"
		drawItems := rawArray(major["items"])
		for _, value := range drawItems {
			entry := rawObject(value)
			if entry == nil {
				continue
			}
			imageURL := browseImage(rawString(entry["src"]))
			if imageURL == "" {
				continue
			}
			image := content.Image{URL: imageURL, Width: int(rawInt64(entry["width"])), Height: int(rawInt64(entry["height"]))}
			images = append(images, image)
		}
		if len(images) > 0 {
			thumbnail = images[0].URL
		}
		if title == "" {
			title = firstLine(text)
		}
	} else if typ == "DYNAMIC_TYPE_ARTICLE" {
		thumbnail = browseImage(firstString(rawArray(major["covers"])))
		if title == "" {
			title = firstLine(text)
		}
	} else {
		if title == "" {
			title = firstLine(text)
		}
	}
	if title == "" {
		title = dynamicKindTitle(typ)
	}
	if text == "" {
		text = rawString(major["desc"])
	}
	if len(images) > 50 {
		images = images[:50]
	}
	blocks := make([]content.ArticleBlock, 0, 1+len(images))
	if strings.TrimSpace(text) != "" {
		blocks = append(blocks, content.ArticleBlock{Kind: "paragraph", Text: strings.TrimSpace(text)})
	}
	for _, image := range images {
		blocks = append(blocks, content.ArticleBlock{Kind: "image", URL: image.URL, Width: image.Width, Height: image.Height})
	}
	item := content.Item{ID: "bilibili:dynamic:" + itemID, Platform: "bilibili", PlatformID: "dynamic:" + itemID, Kind: kind, Title: html.UnescapeString(title), Author: authorName, AuthorID: strconv.FormatInt(authorID, 10), AuthorAvatarURL: authorFace, URL: baseURL, ThumbnailURL: thumbnail, Description: strings.TrimSpace(text), PreviewText: strings.TrimSpace(text), ArticleBlocks: blocks, Images: images, PublishedAt: published, PublishedText: publishedText}
	if item.AuthorID == "0" {
		item.AuthorID = ""
	}
	if item.Title == "" {
		item.Title = "B站视频动态"
	}
	return item, content.Validate(item) == nil
}

func rawObject(raw json.RawMessage) map[string]json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var value map[string]json.RawMessage
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	return value
}

func rawArray(raw json.RawMessage) []json.RawMessage {
	var value []json.RawMessage
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	return value
}

func rawString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text)
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String()
	}
	return ""
}

func rawInt64(raw json.RawMessage) int64 {
	text := rawString(raw)
	if text == "" {
		return 0
	}
	value, _ := strconv.ParseInt(text, 10, 64)
	return value
}

func flexCountValue(raw json.RawMessage) int64 {
	var value flexCount
	_ = value.UnmarshalJSON(raw)
	return int64(value)
}

func firstString(values []json.RawMessage) string {
	if len(values) == 0 {
		return ""
	}
	return rawString(values[0])
}

func dynamicText(value map[string]json.RawMessage) string {
	if value == nil {
		return ""
	}
	return rawString(value["text"])
}

func dynamicURL(raw string) string {
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" {
		return ""
	}
	return raw
}

func firstLine(text string) string {
	text = strings.TrimSpace(text)
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		text = text[:index]
	}
	runes := []rune(text)
	return strings.TrimSpace(string(runes[:minInt(len(runes), 80)]))
}

func dynamicKindTitle(typ string) string {
	if name := dynamicKindNames[typ]; name != "" {
		return "B站" + name + "动态"
	}
	return "B站关注动态"
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (c *Client) live(ctx context.Context, page, area int) (BrowsePage, error) {
	if area != 1 && area != 2 && area != 9 {
		return BrowsePage{}, discovery.ErrInvalidQuery
	}
	var raw json.RawMessage
	params := url.Values{"platform": {"web"}, "parent_area_id": {strconv.Itoa(area)}, "cate_id": {"0"}, "area_id": {"0"}, "sort_type": {"online"}, "page": {strconv.Itoa(page)}, "page_size": {"20"}}
	if err := c.getFrom(ctx, "https://api.live.bilibili.com", "https://live.bilibili.com/", "/room/v1/Area/getRoomList", params, &raw, false); err != nil {
		return BrowsePage{}, err
	}
	type room struct {
		ID          int64  `json:"roomid"`
		OtherID     int64  `json:"room_id"`
		Title       string `json:"title"`
		Author      string `json:"uname"`
		Cover       string `json:"cover"`
		SystemCover string `json:"system_cover"`
		UserCover   string `json:"user_cover"`
	}
	var rooms []room
	if json.Unmarshal(raw, &rooms) != nil {
		var data struct {
			List []room `json:"list"`
		}
		if json.Unmarshal(raw, &data) != nil {
			return BrowsePage{}, discovery.ErrUnavailable
		}
		rooms = data.List
	}
	if rooms == nil || len(rooms) > 100 {
		return BrowsePage{}, discovery.ErrUnavailable
	}
	result := BrowsePage{Items: []content.Item{}, Rooms: []LiveRoom{}, Page: page, SourceTitle: "B站直播", HasMore: len(rooms) == 20}
	seen := map[int64]bool{}
	for _, v := range rooms {
		id := v.ID
		if id <= 0 {
			id = v.OtherID
		}
		if id <= 0 || strings.TrimSpace(v.Title) == "" || seen[id] {
			continue
		}
		cover := v.Cover
		if cover == "" {
			cover = v.SystemCover
		}
		if cover == "" {
			cover = v.UserCover
		}
		cover = secureImage(cover)
		u, err := url.Parse(cover)
		if cover != "" && (err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil) {
			cover = ""
		}
		seen[id] = true
		value := strconv.FormatInt(id, 10)
		result.Rooms = append(result.Rooms, LiveRoom{ID: value, Title: html.UnescapeString(v.Title), Author: v.Author, Thumbnail: cover, URL: "https://live.bilibili.com/" + value})
	}
	return result, nil
}
