package bilibili

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"tangentry/internal/playback"
)

// Resolve the first part as a combined H.264/AAC MP4. DASH's separate audio and
// video tracks must never be mistaken for a browser-playable combined file.
func (c *Client) Playback(ctx context.Context, bvid string) (playback.Source, error) {
	var view struct {
		CID      int64 `json:"cid"`
		Duration int   `json:"duration"`
		Pages    []struct {
			CID      int64 `json:"cid"`
			Duration int   `json:"duration"`
		} `json:"pages"`
	}
	if err := c.get(ctx, "/x/web-interface/view", url.Values{"bvid": {bvid}}, &view, false); err != nil {
		return c.fallbackPlayback(ctx, bvid, err)
	}
	if view.CID <= 0 && len(view.Pages) > 0 {
		view.CID = view.Pages[0].CID
		if view.Duration <= 0 {
			view.Duration = view.Pages[0].Duration
		}
	}
	if view.CID <= 0 {
		return c.fallbackPlayback(ctx, bvid, playback.ErrUnavailable)
	}
	var lastErr error = playback.ErrUnavailable
	// Prefer the requested quality, then step down when the account or region
	// cannot access it. The first valid combined stream wins.
	for _, quality := range []string{"80", "64", "32", "16"} {
		var stream struct {
			Format  string `json:"format"`
			Quality int    `json:"quality"`
			DURL    []struct {
				URL     string   `json:"url"`
				Backups []string `json:"backup_url"`
			} `json:"durl"`
			IsPreview int `json:"is_preview"`
		}
		params := url.Values{"bvid": {bvid}, "cid": {strconv.FormatInt(view.CID, 10)}, "qn": {quality}, "fnval": {"1"}, "fnver": {"0"}, "platform": {"html5"}, "high_quality": {"1"}}
		if err := c.get(ctx, "/x/player/playurl", params, &stream, false); err != nil {
			lastErr = err
			continue
		}
		if stream.IsPreview != 0 || !strings.HasPrefix(stream.Format, "mp4") || len(stream.DURL) != 1 || stream.DURL[0].URL == "" {
			lastErr = playback.ErrUnavailable
			continue
		}
		return playback.Source{URL: stream.DURL[0].URL, Quality: stream.Quality, Duration: view.Duration, Backups: stream.DURL[0].Backups}, nil
	}
	return c.fallbackPlayback(ctx, bvid, lastErr)
}

func (c *Client) fallbackPlayback(ctx context.Context, bvid string, original error) (playback.Source, error) {
	if ctx.Err() != nil {
		return playback.Source{}, ctx.Err()
	}
	if c.playbackFallback != nil {
		source, err := c.playbackFallback.Playback(ctx, bvid)
		if ctx.Err() != nil {
			return playback.Source{}, ctx.Err()
		}
		if err == nil && playback.ValidSource(source) && source.URL != "" {
			return source, nil
		}
	}
	return playback.Source{}, original
}
