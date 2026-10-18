package discourse

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	"tangentry/internal/adapters/feeds"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
)

// Search maps Discourse's /search.json response to the same topic cards used
// by the regular Linux.do feed.
func (c *Client) Search(ctx context.Context, query, cursor string) (discovery.SearchPage, error) {
	page := 1
	if cursor != "" {
		value, err := strconv.Atoi(cursor)
		if err != nil || value < 1 || value > 1000 {
			return discovery.SearchPage{}, discovery.ErrInvalidQuery
		}
		page = value
	}
	raw, _, err := c.read(ctx, "/search.json?q="+url.QueryEscape(query)+"&page="+strconv.Itoa(page))
	if err != nil {
		return discovery.SearchPage{}, err
	}
	var root struct {
		Topics []struct {
			ID       int64  `json:"id"`
			Title    string `json:"title"`
			Excerpt  string `json:"blurb"`
			Username string `json:"username"`
			Created  string `json:"created_at"`
			Replies  int    `json:"reply_count"`
			Views    int64  `json:"views"`
			Likes    int    `json:"like_count"`
		} `json:"topics"`
		More bool `json:"more_full_page_results"`
	}
	if json.Unmarshal(raw, &root) != nil {
		return discovery.SearchPage{}, discovery.ErrUnavailable
	}
	items := make([]content.Item, 0, len(root.Topics))
	for _, topic := range root.Topics {
		if topic.ID <= 0 || strings.TrimSpace(topic.Title) == "" {
			continue
		}
		item := topicItem(topic.ID, topic.Title, topic.Excerpt, topic.Username, topic.Created, topic.Replies, topic.Views, topic.Likes)
		if content.Validate(item) == nil {
			items = append(items, item)
		}
	}
	next := ""
	if root.More {
		next = strconv.Itoa(page + 1)
	}
	return discovery.SearchPage{Items: items, Cursor: next, HasMore: next != "", Platform: "linuxdo"}, nil
}

func topicItem(id int64, title, excerpt, author, created string, replies int, views int64, likes int) content.Item {
	date, _ := time.Parse(time.RFC3339Nano, created)
	return content.Item{ID: "linuxdo:" + strconv.FormatInt(id, 10), Platform: "linuxdo", PlatformID: strconv.FormatInt(id, 10), Kind: "article", Title: title, Author: strings.TrimSpace(author), ReplyCount: replies, ViewCount: views, LikeCount: likes, URL: "https://linux.do/t/" + strconv.FormatInt(id, 10), Description: feeds.Text(excerpt), PreviewText: feeds.Text(excerpt), PublishedAt: date.Unix()}
}
