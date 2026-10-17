package feeds

import (
	"context"
	"strings"
	"sync"
	"tangentry/internal/adapters/articles"
	"tangentry/internal/content"
	"time"
)

type preview struct {
	text    string
	expires time.Time
}

func summaryText(raw string) string {
	text := Text(raw)
	if !content.UsefulDescription(text) {
		return ""
	}
	return text
}

// Use actual article paragraphs only when the feed supplies no useful summary.
// Failures never discard a successful feed update or fetch full text into storage.
func (c *Client) EnrichSummaries(ctx context.Context, items []content.Item) {
	if c.HTTP == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	workers := make(chan struct{}, 4)
	primary := make(map[string]int)
	duplicates := make(map[int]int)
	defer func() {
		for duplicate, original := range duplicates {
			items[duplicate].Description = items[original].Description
		}
	}()
	for i := range items {
		item := &items[i]
		if item.Platform != "rss" || item.Kind != "article" || summaryText(item.Description) != "" {
			continue
		}
		item.Description = ""
		if original, exists := primary[item.URL]; exists {
			duplicates[i] = original
			continue
		}
		primary[item.URL] = i
		c.previewMu.Lock()
		cached := c.previews[item.URL]
		c.previewMu.Unlock()
		if time.Now().Before(cached.expires) {
			item.Description = cached.text
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
			// Other subscriptions may be enriching the same article concurrently.
			for {
				c.previewMu.Lock()
				cached := c.previews[item.URL]
				if time.Now().Before(cached.expires) {
					c.previewMu.Unlock()
					item.Description = cached.text
					return
				}
				if pending := c.previewPending[item.URL]; pending != nil {
					c.previewMu.Unlock()
					select {
					case <-pending:
						continue
					case <-ctx.Done():
						return
					}
				}
				if c.previewPending == nil {
					c.previewPending = make(map[string]chan struct{})
				}
				pending := make(chan struct{})
				c.previewPending[item.URL] = pending
				c.previewMu.Unlock()
				defer func() {
					c.previewMu.Lock()
					delete(c.previewPending, item.URL)
					close(pending)
					c.previewMu.Unlock()
				}()
				break
			}
			itemCtx, stop := context.WithTimeout(ctx, 2*time.Second)
			defer stop()
			doc, err := (&articles.Extractor{HTTP: c.HTTP}).Fetch(itemCtx, item.URL)
			if err == nil {
				for _, block := range doc.Blocks {
					if block.Kind == "paragraph" && len([]rune(block.Text)) >= 40 {
						text := []rune(strings.Join(strings.Fields(block.Text), " "))
						item.Description = string(text[:min(len(text), 280)])
						break
					}
				}
			}
			if ctx.Err() != nil {
				return
			}
			ttl := 5 * time.Minute
			if item.Description != "" {
				ttl = 24 * time.Hour
			}
			c.previewMu.Lock()
			if c.previews == nil || len(c.previews) >= 512 {
				c.previews = make(map[string]preview)
			}
			c.previews[item.URL] = preview{text: item.Description, expires: time.Now().Add(ttl)}
			c.previewMu.Unlock()
		}()
	}
	wg.Wait()
}
