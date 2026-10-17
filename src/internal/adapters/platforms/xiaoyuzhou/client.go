// Package xiaoyuzhou reads episode metadata and the user's official app session.
package xiaoyuzhou

import (
	"context"
	"encoding/json"
	"golang.org/x/net/html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"tangentry/internal/adapters/feeds"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"time"
)

var episodeID = regexp.MustCompile(`^[a-f0-9]{24}$`)

type Client struct {
	HTTP                   *http.Client
	Cookie                 func(string) string
	Auth                   *AuthSession
	recommendationMu       sync.Mutex
	recommendationSessions map[string]*recommendationState
}

func (c *Client) doAuthenticated(ctx context.Context, req *http.Request, token string) (*http.Response, error) {
	if token != "" {
		req.Header.Set("x-jike-access-token", token)
	}
	if c.Auth != nil {
		return c.Auth.Do(ctx, req, token)
	}
	return c.HTTP.Do(req)
}

func URL(raw string) (string, string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Port() != "" || (u.Scheme != "http" && u.Scheme != "https") || (u.Hostname() != "www.xiaoyuzhoufm.com" && u.Hostname() != "xiaoyuzhoufm.com") {
		return "", "", discovery.ErrInvalidQuery
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "episode" || !episodeID.MatchString(parts[1]) {
		return "", "", discovery.ErrInvalidQuery
	}
	return parts[1], "https://www.xiaoyuzhoufm.com/episode/" + parts[1], nil
}
func (c *Client) Episode(ctx context.Context, raw string) (content.Item, error) {
	id, url, err := URL(raw)
	if err != nil {
		return content.Item{}, err
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 Tangentry/0.1")
	if c.Cookie != nil {
		req.Header.Set("Cookie", c.Cookie(url))
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return content.Item{}, &discovery.PlatformError{Message: "小宇宙页面暂时无法连接，请检查网络。"}
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return content.Item{}, &discovery.PlatformError{Message: "小宇宙未能提供这个单集，请检查链接与访问权限。"}
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, (4<<20)+1))
	if err != nil || len(data) > 4<<20 {
		return content.Item{}, discovery.ErrUnavailable
	}
	item, err := Parse(data, id)
	if err != nil {
		return content.Item{}, err
	}
	// Some episodes hide the CDN URL from the public page but expose it to an
	// authenticated app session. Fill it in when possible; a missing URL is
	// still a valid episode and remains readable on the original site.
	if item.MediaURL == "" && c.Auth != nil {
		if media := c.episodeMedia(ctx, id); media != "" {
			item.MediaURL = media
		}
	}
	return item, nil
}

func (c *Client) episodeMedia(ctx context.Context, id string) string {
	access, _ := c.Auth.Tokens()
	if access == "" {
		return ""
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.xiaoyuzhoufm.com/v1/episode/get?eid="+url.QueryEscape(id), nil)
	setAppHeaders(req, access)
	res, err := c.doAuthenticated(ctx, req, access)
	if err != nil || res == nil {
		return ""
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		return ""
	}
	var value struct {
		Data struct {
			ID        string `json:"eid"`
			Private   bool   `json:"isPrivateMedia"`
			Enclosure struct {
				URL string `json:"url"`
			} `json:"enclosure"`
			Media struct {
				URL      string `json:"url"`
				MediaURL string `json:"mediaUrl"`
				Source   struct {
					URL string `json:"url"`
				} `json:"source"`
			} `json:"media"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	e := value.Data
	if e.ID != id || e.Private {
		return ""
	}
	for _, raw := range []string{e.Enclosure.URL, e.Media.Source.URL, e.Media.URL, e.Media.MediaURL} {
		if validAudioURL(raw) {
			return raw
		}
	}
	return ""
}

func validAudioURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.User == nil && u.Port() == "" &&
		(u.Hostname() == "xyzcdn.net" || strings.HasSuffix(u.Hostname(), ".xyzcdn.net") ||
			u.Hostname() == "tk.wavpub.com")
}
func Parse(data []byte, id string) (content.Item, error) {
	z := html.NewTokenizer(strings.NewReader(string(data)))
	var payload, publicAudio string
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			break
		}
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			continue
		}
		token := z.Token()
		if token.Data == "meta" {
			property, value := "", ""
			for _, attr := range token.Attr {
				switch attr.Key {
				case "property", "name":
					property = strings.ToLower(attr.Val)
				case "content":
					value = attr.Val
				}
			}
			if (property == "og:audio" || property == "og:audio:secure_url") && validAudioURL(value) {
				publicAudio = value
			}
		}
		if token.Data != "script" {
			continue
		}
		found := false
		for _, a := range token.Attr {
			if a.Key == "id" && a.Val == "__NEXT_DATA__" {
				found = true
			}
		}
		if found && payload == "" && z.Next() == html.TextToken {
			payload = string(z.Text())
		}
	}
	var value struct {
		Props struct {
			PageProps struct {
				Episode struct {
					ID          string `json:"eid"`
					Title       string `json:"title"`
					Description string `json:"description"`
					Shownotes   string `json:"shownotes"`
					Duration    int    `json:"duration"`
					PubDate     string `json:"pubDate"`
					Private     bool   `json:"isPrivateMedia"`
					Enclosure   struct {
						URL string `json:"url"`
					} `json:"enclosure"`
					Image    any    `json:"image"`
					ImageURL string `json:"imageUrl"`
					Media    struct {
						Enclosure struct {
							URL string `json:"url"`
						} `json:"enclosure"`
						URL      string `json:"url"`
						MediaURL string `json:"mediaUrl"`
						Source   struct {
							Mode string `json:"mode"`
							URL  string `json:"url"`
						} `json:"source"`
					} `json:"media"`
					Podcast struct {
						Title string `json:"title"`
						ID    string `json:"pid"`
						Image any    `json:"image"`
					} `json:"podcast"`
				} `json:"episode"`
			} `json:"pageProps"`
		} `json:"props"`
	}
	if json.Unmarshal([]byte(payload), &value) != nil || value.Props.PageProps.Episode.ID != id {
		return content.Item{}, &discovery.PlatformError{Message: "这个小宇宙单集的公开页面结构暂时无法读取。"}
	}
	e := value.Props.PageProps.Episode
	body := e.Shownotes
	if body == "" {
		body = e.Description
	}
	published, _ := time.Parse(time.RFC3339Nano, e.PubDate)
	var ts int64
	if !published.IsZero() {
		ts = published.Unix()
	}
	media := ""
	if !e.Private {
		candidates := []string{e.Enclosure.URL, e.Media.Enclosure.URL, e.Media.URL, e.Media.MediaURL}
		if e.Media.Source.Mode == "PUBLIC" {
			candidates = append(candidates, e.Media.Source.URL)
		}
		candidates = append(candidates, publicAudio)
		for _, candidate := range candidates {
			if validAudioURL(candidate) {
				media = candidate
				break
			}
		}
	}
	image := episodeImage(map[string]any{
		"image": e.Image, "imageUrl": e.ImageURL,
		"podcast": map[string]any{"image": e.Podcast.Image},
	})
	item := content.Item{ID: "xiaoyuzhou:" + id, Platform: "xiaoyuzhou", PlatformID: id, Kind: "audio", Title: feeds.Text(e.Title), Author: feeds.Text(e.Podcast.Title), AuthorID: e.Podcast.ID, URL: "https://www.xiaoyuzhoufm.com/episode/" + id, ThumbnailURL: image, Description: feeds.Text(body), DurationSeconds: e.Duration, PublishedAt: ts, MediaURL: media}
	if content.Validate(item) != nil {
		return content.Item{}, discovery.ErrUnavailable
	}
	return item, nil
}
