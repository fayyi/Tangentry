// Package xiaohongshu maps one public note page; it does not call private APIs.
package xiaohongshu

import (
	"net/http"
	"sync"

	"tangentry/internal/content"
)

type Client struct {
	HTTP     *http.Client
	Cookie   func(string) string
	mu       sync.Mutex
	locators map[string]string
	order    []string
}

// Keep successful, unsaved video locators bounded for explicit transcription in
// the same service session. Saved content retains its own original share URL.
func (c *Client) VideoLocator(id string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if raw := c.locators[id]; raw != "" {
		return raw
	}
	return "https://www.xiaohongshu.com/explore/" + id
}

func (c *Client) remember(item content.Item) {
	if item.Kind != "video" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.locators == nil {
		c.locators = map[string]string{}
	}
	if _, exists := c.locators[item.PlatformID]; !exists {
		if len(c.order) == 128 {
			delete(c.locators, c.order[0])
			c.order = c.order[1:]
		}
		c.order = append(c.order, item.PlatformID)
	}
	c.locators[item.PlatformID] = item.URL
}
