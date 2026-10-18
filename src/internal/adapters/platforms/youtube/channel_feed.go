// Package youtube maps public channel feeds to Tangentry video identities.
package youtube

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"tangentry/internal/adapters/feeds"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"time"
)

type author struct {
	Name string `xml:"http://www.w3.org/2005/Atom name"`
	URI  string `xml:"http://www.w3.org/2005/Atom uri"`
}
type link struct {
	Rel string `xml:"rel,attr"`
	URL string `xml:"href,attr"`
}
type entry struct {
	XMLName   xml.Name `xml:"http://www.w3.org/2005/Atom entry"`
	ID        string   `xml:"http://www.w3.org/2005/Atom id"`
	VideoID   string   `xml:"http://www.youtube.com/xml/schemas/2015 videoId"`
	ChannelID string   `xml:"http://www.youtube.com/xml/schemas/2015 channelId"`
	Title     string   `xml:"http://www.w3.org/2005/Atom title"`
	Links     []link   `xml:"http://www.w3.org/2005/Atom link"`
	Author    author   `xml:"http://www.w3.org/2005/Atom author"`
	Published string   `xml:"http://www.w3.org/2005/Atom published"`
	Media     struct {
		Description string `xml:"http://search.yahoo.com/mrss/ description"`
		Thumbnail   struct {
			URL string `xml:"url,attr"`
		} `xml:"http://search.yahoo.com/mrss/ thumbnail"`
	} `xml:"http://search.yahoo.com/mrss/ group"`
}
type feed struct {
	XMLName   xml.Name `xml:"http://www.w3.org/2005/Atom feed"`
	ChannelID string   `xml:"http://www.youtube.com/xml/schemas/2015 channelId"`
	Title     string   `xml:"http://www.w3.org/2005/Atom title"`
	Author    author   `xml:"http://www.w3.org/2005/Atom author"`
	Entries   []entry  `xml:"entry"`
}
type Client struct {
	feeds     *feeds.Client
	http      *http.Client
	mu        sync.Mutex
	durations map[string]int
}

func New(httpClient *http.Client) *Client {
	return &Client{feeds: &feeds.Client{HTTP: httpClient, Parser: Parse}, http: httpClient, durations: make(map[string]int)}
}
func (c *Client) FetchConditional(ctx context.Context, id string, v feeds.Validators) (feeds.Result, error) {
	if !discovery.YouTubeChannelID(id) {
		return feeds.Result{}, discovery.ErrInvalidQuery
	}
	return c.feeds.FetchConditional(ctx, discovery.YouTubeChannelFeed(id), v)
}

func (c *Client) Creator(ctx context.Context, id, cursor string) (discovery.SearchPage, error) {
	if !discovery.YouTubeChannelID(id) {
		return discovery.SearchPage{}, discovery.ErrInvalidQuery
	}
	if cursor != "" {
		return discovery.SearchPage{}, discovery.ErrInvalidQuery
	}
	result, err := c.FetchConditional(ctx, id, feeds.Validators{})
	if err != nil {
		return discovery.SearchPage{}, err
	}
	return discovery.SearchPage{Items: result.Feed.Items, Platform: "youtube"}, nil
}
func Parse(data []byte, rawURL string) (discovery.Feed, error) {
	reference, err := discovery.ParseYouTubeChannel(rawURL)
	if err != nil || reference.ID == "" || rawURL != discovery.YouTubeChannelFeed(reference.ID) {
		return discovery.Feed{}, discovery.ErrInvalidQuery
	}
	invalid := func() (discovery.Feed, error) {
		return discovery.Feed{}, &discovery.PlatformError{Message: "YouTube 频道更新返回了不完整或不匹配的内容，上次成功内容已保留。"}
	}
	var document feed
	decoder := xml.NewDecoder(bytes.NewReader(data))
	if decoder.Decode(&document) != nil || len(document.Entries) > 100 {
		return invalid()
	}
	// Reject additional XML documents rather than silently accepting a valid prefix.
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return invalid()
		}
		if text, ok := token.(xml.CharData); !ok || strings.TrimSpace(string(text)) != "" {
			return invalid()
		}
	}
	id := reference.ID
	// YouTube uses the prefixless channel ID at feed level and full UC ID per entry.
	matches := func(raw string) bool { return raw == id || raw == strings.TrimPrefix(id, "UC") }
	if !matches(document.ChannelID) || document.Author.URI == "" {
		return invalid()
	}
	owner, err := discovery.ParseYouTubeChannel(document.Author.URI)
	if err != nil || owner.ID != id {
		return invalid()
	}
	title := feeds.Text(document.Title)
	if title == "" {
		return invalid()
	}
	result := discovery.Feed{Items: []content.Item{}, Page: 1, SourceTitle: title, Query: id}
	seen := map[string]bool{}
	for _, e := range document.Entries {
		videoID, canonical, err := discovery.ParseYouTube("https://www.youtube.com/watch?v=" + e.VideoID)
		if err != nil || e.ID != "yt:video:"+videoID || !matches(e.ChannelID) || seen[videoID] {
			return invalid()
		}
		verifiedLink := false
		for _, l := range e.Links {
			if l.Rel != "alternate" {
				continue
			}
			linked, _, err := discovery.ParseYouTube(l.URL)
			if err != nil || linked != videoID {
				return invalid()
			}
			verifiedLink = true
		}
		if !verifiedLink {
			return invalid()
		}
		if e.Author.URI != "" {
			owner, err := discovery.ParseYouTubeChannel(e.Author.URI)
			if err != nil || owner.ID != id {
				return invalid()
			}
		}
		at, err := time.Parse(time.RFC3339, e.Published)
		if err != nil || at.Unix() < 0 {
			return invalid()
		}
		itemTitle := feeds.Text(e.Title)
		if itemTitle == "" {
			return invalid()
		}
		author := feeds.Text(e.Author.Name)
		if author == "" {
			author = feeds.Text(document.Author.Name)
		}
		if author == "" {
			author = title
		}
		thumb := ""
		if e.Media.Thumbnail.URL != "" {
			u, err := url.Parse(e.Media.Thumbnail.URL)
			if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || !strings.HasSuffix(strings.ToLower(u.Hostname()), ".ytimg.com") {
				return invalid()
			}
			thumb = u.String()
		}
		item := content.Item{ID: "youtube:" + videoID, Platform: "youtube", PlatformID: videoID, Kind: "video", Title: itemTitle, Author: author, AuthorID: id, URL: canonical, ThumbnailURL: thumb, Description: feeds.Text(e.Media.Description), PublishedAt: at.Unix()}
		if content.Validate(item) != nil {
			return invalid()
		}
		seen[videoID] = true
		result.Items = append(result.Items, item)
	}
	result.Total = len(result.Items)
	return result, nil
}
