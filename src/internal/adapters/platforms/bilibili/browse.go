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
	item := content.Item{ID: "bilibili:" + v.BVID, Platform: "bilibili", PlatformID: v.BVID, Kind: "video", Title: html.UnescapeString(v.Title), Author: v.Owner.Name, URL: "https://www.bilibili.com/video/" + v.BVID + "/", ThumbnailURL: secureImage(v.Picture), AuthorAvatarURL: secureImage(v.Owner.Face), Description: v.Description, DurationSeconds: v.Duration, PublishedAt: v.Published}
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

func (c *Client) dynamic(ctx context.Context, page int, cursor string) (BrowsePage, error) {
	if page > 1 && cursor == "" {
		return BrowsePage{}, discovery.ErrInvalidQuery
	}
	key, err := c.mixinKey(ctx)
	if err != nil {
		return BrowsePage{}, err
	}
	params := url.Values{}
	if cursor != "" {
		params.Set("offset", cursor)
	}
	sign(params, key, time.Now().Unix())
	var data struct {
		Items   []json.RawMessage `json:"items"`
		Offset  string            `json:"offset"`
		HasMore json.RawMessage   `json:"has_more"`
	}
	if err := c.getFrom(ctx, c.baseURL, "https://t.bilibili.com/", "/x/polymer/web-dynamic/v1/feed/nav", params, &data, false); err != nil {
		return BrowsePage{}, err
	}
	if data.Items == nil || len(data.Items) > 100 || len(data.Offset) > 256 {
		return BrowsePage{}, discovery.ErrUnavailable
	}
	hasMore := string(data.HasMore) == "1" || string(data.HasMore) == "true"
	result := BrowsePage{Items: []content.Item{}, Page: page, SourceTitle: "关注动态", HasMore: hasMore && data.Offset != "" && data.Offset != cursor, Next: data.Offset}
	seen := map[string]bool{}
	for _, raw := range data.Items {
		if item, ok := dynamicVideo(raw, 0); ok && !seen[item.ID] {
			result.Items = append(result.Items, item)
			seen[item.ID] = true
		}
	}
	return result, nil
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
		Author    struct {
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
						BVID        string `json:"bvid"`
						Title       string `json:"title"`
						Cover       string `json:"cover"`
						Description string `json:"desc"`
						Duration    string `json:"duration_text"`
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
		v.Owner.MID, v.Owner.Name, v.Owner.Face = data.Author.MID, data.Author.Name, data.Author.Face
		if v.Owner.MID <= 0 {
			v.Owner.MID = data.UID
		}
	} else {
		a := data.Modules.Dynamic.Major.Archive
		v.BVID, v.Title, v.Picture, v.Description, v.Duration = a.BVID, a.Title, a.Cover, a.Description, duration(a.Duration)
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
