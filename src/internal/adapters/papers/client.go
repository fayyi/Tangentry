// Package papers adapts the public arXiv and Hugging Face paper APIs.
package papers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"tangentry/internal/netpolicy"
	domain "tangentry/internal/papers"
)

const maxResponseBytes = 16 << 20

type Client struct {
	HTTP             *http.Client
	UserAgent        string
	MinArxivInterval time.Duration
	requestMu        sync.Mutex
	lastArxivRequest time.Time
}

func (c *Client) Fetch(ctx context.Context, id string) (content.Item, error) {
	ref, err := domain.ParseReference(id)
	if err != nil {
		return content.Item{}, discovery.ErrInvalidQuery
	}
	endpoint := "https://export.arxiv.org/api/query?id_list=" + url.QueryEscape(ref.ID)
	data, err := c.get(ctx, endpoint, true)
	if err != nil {
		return content.Item{}, err
	}
	items, err := domain.ParseArxivAtom(data)
	if err != nil || len(items) == 0 {
		return content.Item{}, &discovery.PlatformError{Message: "arXiv 暂时无法读取论文元数据。"}
	}
	for _, paper := range items {
		if paper.ID == ref.ID {
			if ref.Version > 0 {
				paper.Version = ref.Version
				paper.PDFURL = "https://arxiv.org/pdf/" + paper.ID + "v" + fmt.Sprint(ref.Version) + ".pdf"
				paper.ArxivURL = "https://arxiv.org/abs/" + paper.ID + "v" + fmt.Sprint(ref.Version)
			}
			return domain.ToItem(paper), nil
		}
	}
	return content.Item{}, discovery.ErrInvalidQuery
}

// Daily returns the selected HF daily list. If the selected date has no papers,
// it retries the previous calendar date, matching the website's fallback.
func (c *Client) Daily(ctx context.Context, date time.Time) (discovery.Feed, error) {
	for offset := 0; offset < 2; offset++ {
		day := date.AddDate(0, 0, -offset)
		endpoint := "https://huggingface.co/api/daily_papers?date=" + day.Format("2006-01-02")
		data, err := c.get(ctx, endpoint, false)
		if err != nil {
			if offset == 0 {
				continue
			}
			return discovery.Feed{}, err
		}
		papers, parseErr := domain.ParseHFDaily(data)
		if parseErr != nil || len(papers) == 0 {
			continue
		}
		return feedFromPapers(papers, "HF 每日论文", day.Format("2006-01-02")), nil
	}
	return discovery.Feed{}, &discovery.PlatformError{Message: "Hugging Face 每日论文暂时没有可用内容。"}
}

func (c *Client) ArxivCategory(ctx context.Context, category string) (discovery.Feed, error) {
	var err error
	category, err = domain.NormalizeCategory(strings.TrimSpace(category))
	if err != nil {
		return discovery.Feed{}, discovery.ErrInvalidQuery
	}
	endpoint := "https://rss.arxiv.org/rss/" + category
	data, err := c.get(ctx, endpoint, true)
	if err != nil {
		return discovery.Feed{}, err
	}
	items, err := domain.ParseArxivRSS(data)
	if err != nil {
		return discovery.Feed{}, &discovery.PlatformError{Message: "arXiv 分类订阅暂时无法读取。"}
	}
	return feedFromPapers(items, "arXiv "+category, category), nil
}

func feedFromPapers(items []domain.Paper, title, query string) discovery.Feed {
	feed := discovery.Feed{Page: 1, Total: len(items), SourceTitle: title, Query: query, Items: make([]content.Item, 0, len(items))}
	for _, paper := range items {
		item := domain.ToItem(paper)
		if content.Validate(item) == nil {
			feed.Items = append(feed.Items, item)
		}
	}
	feed.Total = len(feed.Items)
	return feed
}

func (c *Client) get(ctx context.Context, endpoint string, arxiv bool) ([]byte, error) {
	canonical, err := netpolicy.URL(endpoint)
	if err != nil {
		return nil, discovery.ErrInvalidQuery
	}
	if arxiv {
		c.waitArxiv(ctx)
	}
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, canonical, nil)
	if err != nil {
		return nil, err
	}
	ua := c.UserAgent
	if ua == "" {
		ua = "Tangentry/0.1 (paper reader)"
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "application/atom+xml, application/rss+xml, application/json, text/xml")
	response, err := httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &discovery.PlatformError{Message: "论文服务暂时无法连接。"}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, &discovery.PlatformError{Message: "论文服务返回 HTTP " + fmt.Sprint(response.StatusCode) + "。"}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(data) > maxResponseBytes {
		return nil, &discovery.PlatformError{Message: "论文服务返回内容过大或读取失败。"}
	}
	return data, nil
}

func (c *Client) waitArxiv(ctx context.Context) {
	interval := c.MinArxivInterval
	if interval == 0 {
		interval = 3 * time.Second
	}
	for {
		c.requestMu.Lock()
		wait := interval - time.Since(c.lastArxivRequest)
		if wait <= 0 {
			c.lastArxivRequest = time.Now()
			c.requestMu.Unlock()
			return
		}
		c.requestMu.Unlock()
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
