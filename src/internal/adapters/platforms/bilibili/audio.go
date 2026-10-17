package bilibili

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"tangentry/internal/discovery"
	"tangentry/internal/playback"
	"tangentry/internal/transcription"
)

// Speech recognition needs an audio track, not a full combined video download.
// Keep this resolver separate from the browser's combined H.264/AAC playback.
func (c *Client) Audio(ctx context.Context, bvid string) (transcription.Audio, error) {
	var view struct {
		CID      int64 `json:"cid"`
		Duration int   `json:"duration"`
	}
	if err := c.get(ctx, "/x/web-interface/view", url.Values{"bvid": {bvid}}, &view, false); err != nil {
		return transcription.Audio{}, audioResolveError(err)
	}
	if view.CID <= 0 {
		return transcription.Audio{}, transcription.ErrInvalid
	}
	var stream struct {
		Preview int `json:"is_preview"`
		DASH    struct {
			Audio []struct {
				URL              string   `json:"baseUrl"`
				Alternate        string   `json:"base_url"`
				Backups          []string `json:"backupUrl"`
				AlternateBackups []string `json:"backup_url"`
				Codec            string   `json:"codecs"`
				Bandwidth        int      `json:"bandwidth"`
			} `json:"audio"`
		} `json:"dash"`
	}
	params := url.Values{"bvid": {bvid}, "cid": {strconv.FormatInt(view.CID, 10)}, "qn": {"80"}, "fnval": {"16"}, "fnver": {"0"}}
	if err := c.get(ctx, "/x/player/playurl", params, &stream, false); err != nil {
		return transcription.Audio{}, audioResolveError(err)
	}
	if stream.Preview != 0 {
		return transcription.Audio{}, transcription.Failed("source_preview", "平台仅提供试看音轨，不能转写完整视频。")
	}
	var selected transcription.Audio
	bandwidth := 0
	for _, candidate := range stream.DASH.Audio {
		raw := candidate.URL
		if raw == "" {
			raw = candidate.Alternate
		}
		if !strings.HasPrefix(candidate.Codec, "mp4a") || !playback.ValidSource(playback.Source{URL: raw}) {
			continue
		}
		if selected.URL != "" && (candidate.Bandwidth <= 0 || (bandwidth > 0 && candidate.Bandwidth >= bandwidth)) {
			continue
		}
		selected = transcription.Audio{URL: raw, Duration: view.Duration, Referer: "https://www.bilibili.com/", Platform: "bilibili"}
		bandwidth = candidate.Bandwidth
		for _, backup := range append(candidate.Backups, candidate.AlternateBackups...) {
			if backup != raw && playback.ValidSource(playback.Source{URL: backup}) {
				selected.Backups = append(selected.Backups, backup)
				if len(selected.Backups) == 2 {
					break
				}
			}
		}
	}
	if selected.URL != "" {
		return selected, nil
	}
	// Older videos may have no DASH audio. Only then use combined MP4.
	source, err := c.Playback(ctx, bvid)
	if err != nil {
		return transcription.Audio{}, audioResolveError(err)
	}
	if !playback.ValidSource(source) {
		return transcription.Audio{}, transcription.Failed("source_unavailable", "平台没有提供可用的音轨。")
	}
	return transcription.Audio{URL: source.URL, Duration: source.Duration, Referer: "https://www.bilibili.com/", Backups: source.Backups, Platform: "bilibili"}, nil
}

func audioResolveError(err error) error {
	var platform *discovery.PlatformError
	if errors.As(err, &platform) {
		return transcription.Failed("source_unavailable", platform.Message)
	}
	return err
}
