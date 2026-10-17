package youtube

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"tangentry/internal/content"
	"time"

	"golang.org/x/net/html"
)

var isoDuration = regexp.MustCompile(`^PT(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?$`)

// Public Atom feeds omit duration. Enrichment is bounded and best effort:
// unavailable watch pages must never discard a successful channel update.
func (c *Client) EnrichDurations(ctx context.Context, items []content.Item) {
	if c.http == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	workers := make(chan struct{}, 4)
	for i := range items {
		item := &items[i]
		if item.DurationSeconds > 0 || item.Platform != "youtube" {
			continue
		}
		c.mu.Lock()
		duration := c.durations[item.PlatformID]
		c.mu.Unlock()
		if duration > 0 {
			item.DurationSeconds = duration
			continue
		}
		select {
		case workers <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-workers }()
			itemCtx, stop := context.WithTimeout(ctx, 2*time.Second)
			defer stop()
			req, err := http.NewRequestWithContext(itemCtx, http.MethodGet, item.URL, nil)
			if err != nil {
				return
			}
			res, err := c.http.Do(req)
			if err != nil {
				return
			}
			defer res.Body.Close()
			if res.StatusCode != http.StatusOK || res.Request == nil || res.Request.URL.Hostname() != "www.youtube.com" || res.Request.URL.Path != "/watch" || res.Request.URL.Query().Get("v") != item.PlatformID {
				return
			}
			duration := watchDuration(io.LimitReader(res.Body, 4<<20))
			if duration == 0 {
				return
			}
			item.DurationSeconds = duration
			c.mu.Lock()
			if len(c.durations) >= 512 {
				clear(c.durations)
			}
			c.durations[item.PlatformID] = duration
			c.mu.Unlock()
		}()
	}
	wg.Wait()
}

func watchDuration(reader io.Reader) int {
	z := html.NewTokenizer(reader)
	for {
		switch z.Next() {
		case html.ErrorToken:
			return 0
		case html.StartTagToken, html.SelfClosingTagToken:
			token := z.Token()
			if token.Data != "meta" {
				continue
			}
			property, value := "", ""
			for _, attr := range token.Attr {
				if attr.Key == "itemprop" {
					property = attr.Val
				}
				if attr.Key == "content" {
					value = attr.Val
				}
			}
			if property != "duration" {
				continue
			}
			parts := isoDuration.FindStringSubmatch(value)
			if parts == nil {
				continue
			}
			total := 0
			for i, factor := range []int{3600, 60, 1} {
				if parts[i+1] == "" {
					continue
				}
				value, err := strconv.Atoi(parts[i+1])
				if err != nil || value > 604800/factor {
					return 0
				}
				total += value * factor
				if total > 604800 {
					return 0
				}
			}
			return total
		}
	}
}
