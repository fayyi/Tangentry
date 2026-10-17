package bilibili

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"sort"
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

// The web feed no longer honours type=video, so a page of follow updates can
// be all images or articles. Keep reading a few pages until there are enough
// videos to show, and explain an empty result instead of a bare "none".
const (
	dynamicFeatures   = "itemOpusStyle,listOnlyfans,opusBigCover,onlyfansVote,decorationCard,onlyfansAssetsV2,forwardListHidden,ugcDelete"
	dynamicTarget     = 6
	dynamicMaxBatches = 4
)

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
	seen, skipped := map[string]bool{}, map[string]int{}
	offset, more, scanned := cursor, true, 0
	for batch := 0; batch < dynamicMaxBatches && more && len(result.Items) < dynamicTarget; batch++ {
		params := url.Values{"timezone_offset": {"-480"}, "type": {"video"}, "platform": {"web"}, "features": {dynamicFeatures}, "web_location": {"333.1365"}}
		if offset != "" {
			params.Set("offset", offset)
		}
		sign(params, key, time.Now().Unix())
		var data struct {
			Items   []json.RawMessage `json:"items"`
			Offset  string            `json:"offset"`
			HasMore json.RawMessage   `json:"has_more"`
		}
		err := c.getFrom(ctx, c.baseURL, "https://t.bilibili.com/", "/x/polymer/web-dynamic/v1/feed/all", params, &data, false)
		if err == nil && (data.Items == nil || len(data.Items) > 100 || len(data.Offset) > 256) {
			err = discovery.ErrUnavailable
		}
		if err != nil {
			if batch > 0 {
				break
			}
			return BrowsePage{}, err
		}
		scanned += len(data.Items)
		for _, raw := range data.Items {
			item, ok := dynamicVideo(raw, 0)
			if !ok {
				var head struct {
					Type string `json:"type"`
				}
				_ = json.Unmarshal(raw, &head)
				skipped[head.Type]++
				continue
			}
			if !seen[item.ID] {
				result.Items = append(result.Items, item)
				seen[item.ID] = true
			}
		}
		more = dynamicHasMore(data.HasMore) && data.Offset != "" && data.Offset != offset
		offset = data.Offset
	}
	result.HasMore, result.Next = more, offset
	if len(result.Items) == 0 {
		result.Notice = dynamicNotice(scanned, skipped, more)
	}
	return result, nil
}

func dynamicNotice(scanned int, skipped map[string]int, more bool) string {
	if scanned == 0 {
		return "B站没有返回任何关注动态。请确认账号已关注 UP 主；若刚登录，可在设置里重新扫码登录后再试。"
	}
	kinds := make([]string, 0, len(skipped))
	for kind, count := range skipped {
		name := dynamicKindNames[kind]
		if name == "" {
			name = "其他"
		}
		kinds = append(kinds, fmt.Sprintf("%s %d", name, count))
	}
	sort.Strings(kinds)
	notice := fmt.Sprintf("已查看最近 %d 条关注动态，其中没有视频投稿（%s）。", scanned, strings.Join(kinds, "、"))
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
	if depth > 2 {
		return content.Item{}, false
	}
	var data struct {
		Type      json.RawMessage `json:"type"`
		BVID      string          `json:"bvid"`
		Title     string          `json:"title"`
		Cover     string          `json:"cover"`
		JumpURL   string          `json:"jump_url"`
		UID       int64           `json:"uid"`
		Published int64           `json:"pub_ts"`
		Duration  string          `json:"duration_text"`
		ViewCount flexCount       `json:"view_count"`
		Stat      struct {
			View flexCount `json:"view"`
			Play flexCount `json:"play"`
		} `json:"stat"`
		Author struct {
			MID  int64  `json:"mid"`
			Name string `json:"name"`
			Face string `json:"face"`
		} `json:"author"`
		Original json.RawMessage `json:"orig"`
		Modules  struct {
			Author struct {
				MID       int64  `json:"mid"`
				Name      string `json:"name"`
				Face      string `json:"face"`
				Published int64  `json:"pub_ts"`
			} `json:"module_author"`
			Dynamic struct {
				Major struct {
					Archive struct {
						BVID        string    `json:"bvid"`
						Title       string    `json:"title"`
						Cover       string    `json:"cover"`
						Description string    `json:"desc"`
						Duration    string    `json:"duration_text"`
						ViewCount   flexCount `json:"view_count"`
						Stat        struct {
							View flexCount `json:"view"`
							Play flexCount `json:"play"`
						} `json:"stat"`
					} `json:"archive"`
				} `json:"major"`
			} `json:"module_dynamic"`
		} `json:"modules"`
	}
	if json.Unmarshal(raw, &data) != nil {
		return content.Item{}, false
	}
	if string(data.Type) == "1" || string(data.Type) == `"1"` || string(data.Type) == `"DYNAMIC_TYPE_FORWARD"` {
		return dynamicVideo(data.Original, depth+1)
	}
	v := browseVideo{}
	if string(data.Type) == "8" || string(data.Type) == `"8"` {
		v.BVID = data.BVID
		if v.BVID == "" {
			jump := data.JumpURL
			if strings.HasPrefix(jump, "//") {
				jump = "https:" + jump
			}
			kind, id, err := discovery.ParseQuery(jump)
			if err == nil && kind == "video" {
				v.BVID = id
			}
		}
		v.Title, v.Picture, v.Duration, v.Published = data.Title, data.Cover, duration(data.Duration), data.Published
		v.ViewCount = int64(data.ViewCount)
		if v.ViewCount == 0 {
			v.ViewCount = int64(data.Stat.View)
		}
		if v.ViewCount == 0 {
			v.ViewCount = int64(data.Stat.Play)
		}
		v.Owner.MID, v.Owner.Name, v.Owner.Face = data.Author.MID, data.Author.Name, data.Author.Face
		if v.Owner.MID <= 0 {
			v.Owner.MID = data.UID
		}
	} else {
		a := data.Modules.Dynamic.Major.Archive
		v.BVID, v.Title, v.Picture, v.Description, v.Duration = a.BVID, a.Title, a.Cover, a.Description, duration(a.Duration)
		v.ViewCount = int64(a.ViewCount)
		if v.ViewCount == 0 {
			v.ViewCount = int64(a.Stat.View)
		}
		if v.ViewCount == 0 {
			v.ViewCount = int64(a.Stat.Play)
		}
		v.Published = data.Modules.Author.Published
		v.Owner.MID, v.Owner.Name, v.Owner.Face = data.Modules.Author.MID, data.Modules.Author.Name, data.Modules.Author.Face
	}
	return v.item()
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
