package xiaohongshu

import (
	"encoding/json"
	"net/url"
	"sort"
	"strings"

	"tangentry/internal/content"
)

type noteStream struct {
	URL      string
	Backups  []string
	Height   int
	Width    int
	Duration int
	Format   string
	Codec    string
	Audio    string
}

func (s *noteStream) UnmarshalJSON(data []byte) error {
	var value struct {
		URL         string   `json:"masterUrl"`
		SnakeURL    string   `json:"master_url"`
		Backups     []string `json:"backupUrls"`
		SnakeBackup []string `json:"backup_urls"`
		Height      int      `json:"height"`
		Width       int      `json:"width"`
		Duration    int      `json:"duration"`
		Format      string   `json:"format"`
		Codec       string   `json:"videoCodec"`
		SnakeCodec  string   `json:"video_codec"`
		Audio       string   `json:"audioCodec"`
		SnakeAudio  string   `json:"audio_codec"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	s.URL, s.Backups, s.Height, s.Width, s.Duration, s.Format = value.URL, value.Backups, value.Height, value.Width, value.Duration, value.Format
	if s.URL == "" {
		s.URL = value.SnakeURL
	}
	if len(s.Backups) == 0 {
		s.Backups = value.SnakeBackup
	}
	s.Codec, s.Audio = value.Codec, value.Audio
	if s.Codec == "" {
		s.Codec = value.SnakeCodec
	}
	if s.Audio == "" {
		s.Audio = value.SnakeAudio
	}
	return nil
}

type noteMedia struct {
	Stream map[string][]noteStream `json:"stream"`
	Video  struct {
		DRMType      json.RawMessage `json:"drm_type"`
		DRMTypeCamel json.RawMessage `json:"drmType"`
	} `json:"video"`
}

func protectedMedia(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "false" || string(raw) == `""` || string(raw) == "0" {
		return false
	}
	return true
}

func decodeMediaV2(raw json.RawMessage) (noteMedia, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return noteMedia{}, false
	}
	if len(raw) > 500000 {
		return noteMedia{}, true
	}
	var encoded string
	if json.Unmarshal(raw, &encoded) != nil || len(encoded) > 500000 {
		return noteMedia{}, true
	}
	var media noteMedia
	if json.Unmarshal([]byte(encoded), &media) != nil {
		return noteMedia{}, true
	}
	if protectedMedia(media.Video.DRMType) || protectedMedia(media.Video.DRMTypeCamel) {
		return noteMedia{}, true
	}
	return media, true
}

func imageURL(raw string) string {
	if len(raw) > 4096 {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Port() != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if !strings.HasSuffix(host, ".xhscdn.com") && !strings.HasSuffix(host, ".xhsimg.com") && host != "ci.xiaohongshu.com" {
		return ""
	}
	u.Scheme = "https"
	return u.String()
}

func chooseVideo(item *content.Item, media noteMedia) {
	bestQuality := 0
	groups := make([]string, 0, len(media.Stream))
	for group := range media.Stream {
		groups = append(groups, group)
	}
	sort.Strings(groups)
	for _, group := range groups {
		for _, stream := range media.Stream[group] {
			codec := strings.ToLower(stream.Codec)
			if stream.Audio != "" && strings.ToLower(stream.Audio) != "aac" {
				continue
			}
			format := strings.ToLower(stream.Format)
			// Current pages use EF* groups; choose combined MP4 rather than
			// fragmented media, and let the browser verify the codec.
			if format != "mp4" && !(format == "" && group == "h264") ||
				codec == "h265" || codec == "hevc" || codec == "av1" ||
				stream.Height < 0 || stream.Width < 0 {
				continue
			}
			quality := stream.Height
			if stream.Width > 0 && (quality == 0 || stream.Width < quality) {
				quality = stream.Width
			}
			picked := imageURL(stream.URL)
			if picked == "" {
				for _, backup := range stream.Backups {
					if picked = imageURL(backup); picked != "" {
						break
					}
				}
			}
			if picked != "" && (item.MediaURL == "" ||
				quality <= 1080 && (bestQuality > 1080 || quality > bestQuality) ||
				quality > 1080 && bestQuality > 1080 && quality < bestQuality) {
				item.MediaURL = picked
				bestQuality = quality
				if stream.Duration > 0 {
					item.DurationSeconds = (stream.Duration + 999) / 1000
				}
			}
		}
	}
}
