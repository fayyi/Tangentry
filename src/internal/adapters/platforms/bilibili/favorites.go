package bilibili

import (
	"context"
	"html"
	"net/url"
	"strconv"

	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"tangentry/internal/favoritesync"
)

type favoriteFolder struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	MediaCount *int   `json:"media_count"`
}

func (c *Client) Folders(ctx context.Context) ([]favoritesync.Folder, error) {
	var account struct {
		LoggedIn bool  `json:"isLogin"`
		MID      int64 `json:"mid"`
	}
	if err := c.get(ctx, "/x/web-interface/nav", url.Values{}, &account, false); err != nil {
		return nil, err
	}
	if !account.LoggedIn || account.MID <= 0 {
		return nil, &discovery.PlatformError{Message: "请先在工具设置保存有效的 B站 Cookie，再读取你的收藏夹。"}
	}
	var data struct {
		Count *int             `json:"count"`
		List  []favoriteFolder `json:"list"`
	}
	if err := c.get(ctx, "/x/v3/fav/folder/created/list-all", url.Values{"up_mid": {strconv.FormatInt(account.MID, 10)}, "type": {"2"}}, &data, false); err != nil {
		return nil, err
	}
	if data.Count == nil || *data.Count < 0 || *data.Count != len(data.List) {
		return nil, favoritesync.ErrIncomplete
	}
	result := make([]favoritesync.Folder, 0, len(data.List))
	seen := make(map[int64]bool)
	for _, f := range data.List {
		if f.ID <= 0 || f.Title == "" || f.MediaCount == nil || *f.MediaCount < 0 || seen[f.ID] {
			return nil, favoritesync.ErrIncomplete
		}
		seen[f.ID] = true
		result = append(result, favoritesync.Folder{ID: strconv.FormatInt(f.ID, 10), Title: html.UnescapeString(f.Title), Count: *f.MediaCount})
	}
	return result, nil
}
func (c *Client) Page(ctx context.Context, id string, page int) (favoritesync.Page, error) {
	if parsed, err := favoritesync.ParseFolder(id); err != nil || parsed != id || page < 1 || page > favoritesync.MaxItems/20+1 {
		return favoritesync.Page{}, favoritesync.ErrInvalid
	}
	var data struct {
		Info    favoriteFolder `json:"info"`
		HasMore *bool          `json:"has_more"`
		Medias  []struct {
			Type        int    `json:"type"`
			Attr        int    `json:"attr"`
			BVID        string `json:"bvid"`
			BVIDLegacy  string `json:"bv_id"`
			Title       string `json:"title"`
			Cover       string `json:"cover"`
			Intro       string `json:"intro"`
			Duration    int    `json:"duration"`
			PublishedAt int64  `json:"pubtime"`
			Upper       struct {
				MID  int64  `json:"mid"`
				Name string `json:"name"`
			} `json:"upper"`
		} `json:"medias"`
	}
	params := url.Values{"media_id": {id}, "pn": {strconv.Itoa(page)}, "ps": {"20"}, "order": {"mtime"}, "type": {"0"}, "platform": {"web"}}
	if err := c.get(ctx, "/x/v3/fav/resource/list", params, &data, false); err != nil {
		return favoritesync.Page{}, err
	}
	if data.Info.ID <= 0 || strconv.FormatInt(data.Info.ID, 10) != id || data.Info.Title == "" || data.Info.MediaCount == nil || data.HasMore == nil || len(data.Medias) > 20 {
		return favoritesync.Page{}, favoritesync.ErrIncomplete
	}
	result := favoritesync.Page{Folder: favoritesync.Folder{ID: id, Title: html.UnescapeString(data.Info.Title), Count: *data.Info.MediaCount}, Items: []content.Item{}, HasMore: *data.HasMore, Entries: len(data.Medias)}
	for _, media := range data.Medias {
		if media.Type != 2 || media.Attr != 0 {
			result.Excluded++
			continue
		}
		bvid := media.BVID
		if bvid == "" {
			bvid = media.BVIDLegacy
		}
		if kind, parsed, err := discovery.ParseQuery(bvid); err != nil || kind != "video" || parsed != bvid || media.Title == "" || media.Duration < 0 {
			return favoritesync.Page{}, favoritesync.ErrIncomplete
		}
		result.Items = append(result.Items, content.Item{ID: "bilibili:" + bvid, Platform: "bilibili", PlatformID: bvid, Kind: "video", Title: html.UnescapeString(media.Title), Author: media.Upper.Name, AuthorID: strconv.FormatInt(media.Upper.MID, 10), URL: "https://www.bilibili.com/video/" + bvid + "/", ThumbnailURL: secureImage(media.Cover), Description: media.Intro, DurationSeconds: media.Duration, PublishedAt: media.PublishedAt})
	}
	return result, nil
}
