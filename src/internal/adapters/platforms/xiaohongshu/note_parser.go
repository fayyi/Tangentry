package xiaohongshu

import (
	"encoding/json"
	"strings"

	"golang.org/x/net/html"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
)

type hydratedNote struct {
	ID          string `json:"noteId"`
	Type        string `json:"type"`
	Title       string `json:"title"`
	Description string `json:"desc"`
	Time        int64  `json:"time"`
	User        struct {
		Avatar string `json:"avatar"`
		Name   string `json:"nickname"`
		ID     string `json:"userId"`
	} `json:"user"`
	Images []struct {
		URL     string `json:"urlDefault"`
		Preview string `json:"urlPre"`
		Width   int    `json:"width"`
		Height  int    `json:"height"`
		Info    []struct {
			Scene string `json:"imageScene"`
			URL   string `json:"url"`
		} `json:"infoList"`
	} `json:"imageList"`
	Video struct {
		Media struct {
			Stream map[string][]noteStream `json:"stream"`
		} `json:"media"`
		MediaV2 json.RawMessage `json:"mediaV2"`
	} `json:"video"`
}

// Parse reads only the server-rendered hydration payload. It never evaluates
// page JavaScript and accepts exactly one initializer for the requested note.
func Parse(data []byte, ref discovery.XiaohongshuReference) (content.Item, error) {
	canonical, err := discovery.ParseXiaohongshu(ref.URL)
	if err != nil || ref.ID == "" || canonical.ID != ref.ID {
		return content.Item{}, discovery.ErrInvalidQuery
	}
	z := html.NewTokenizer(strings.NewReader(string(data)))
	var payload []byte
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			break
		}
		if kind != html.StartTagToken || z.Token().Data != "script" || z.Next() != html.TextToken {
			continue
		}
		script := strings.TrimSpace(string(z.Text()))
		const assignment = "window.__INITIAL_STATE__"
		if !strings.HasPrefix(script, assignment) {
			continue
		}
		script = strings.TrimSpace(strings.TrimPrefix(script, assignment))
		if !strings.HasPrefix(script, "=") {
			continue
		}
		candidate, parseErr := stateJSON(strings.TrimSpace(script[1:]))
		if parseErr != nil || payload != nil {
			return content.Item{}, unavailable()
		}
		payload = candidate
	}
	if len(payload) == 0 {
		return content.Item{}, unavailable()
	}
	var state struct {
		Note struct {
			Detail map[string]struct {
				Note hydratedNote `json:"note"`
			} `json:"noteDetailMap"`
		} `json:"note"`
	}
	if json.Unmarshal(payload, &state) != nil {
		return content.Item{}, unavailable()
	}
	entry, ok := state.Note.Detail[ref.ID]
	n := entry.Note
	if !ok || n.ID != ref.ID || (n.Type != "normal" && n.Type != "video") || len(n.Images) > 50 {
		return content.Item{}, unavailable()
	}
	images := make([]content.Image, 0, len(n.Images))
	for _, image := range n.Images {
		picked := imageURL(image.URL)
		if picked == "" {
			for _, info := range image.Info {
				if info.Scene == "WB_DFT" {
					picked = imageURL(info.URL)
					if picked != "" {
						break
					}
				}
			}
		}
		if picked == "" {
			picked = imageURL(image.Preview)
		}
		if picked == "" && n.Type != "video" {
			return content.Item{}, &discovery.PlatformError{Message: "这份笔记的图片地址暂时无法读取，未导入不完整图集。"}
		}
		if picked != "" {
			images = append(images, content.Image{URL: picked, Width: image.Width, Height: image.Height})
		}
	}
	title := strings.TrimSpace(n.Title)
	if title == "" {
		body := []rune(strings.TrimSpace(n.Description))
		title = string(body[:min(60, len(body))])
	}
	if title == "" && len(images) > 0 {
		title = "小红书图片笔记"
	}
	thumbnail := ""
	if len(images) > 0 {
		thumbnail = images[0].URL
	}
	kind := "image"
	if n.Type == "video" {
		kind = "video"
		images = nil
	} else if len(images) == 0 {
		kind = "article"
	}
	published := n.Time
	if published > 100000000000 {
		published /= 1000
	}
	if published < 0 {
		published = 0
	}
	if published > 253402300799 {
		return content.Item{}, unavailable()
	}
	item := content.Item{ID: "xiaohongshu:" + ref.ID, Platform: "xiaohongshu", PlatformID: ref.ID, Kind: kind, Title: title, Author: n.User.Name, AuthorID: n.User.ID, URL: canonical.URL, ThumbnailURL: thumbnail, Description: n.Description, PublishedAt: published, Images: images}
	if kind == "video" {
		media := n.Video.Media
		if v2, supplied := decodeMediaV2(n.Video.MediaV2); supplied {
			media.Stream = v2.Stream
		}
		chooseVideo(&item, noteMedia{Stream: media.Stream})
	}
	item.AuthorAvatarURL = imageURL(n.User.Avatar)
	if content.Validate(item) != nil {
		return content.Item{}, unavailable()
	}
	return item, nil
}
