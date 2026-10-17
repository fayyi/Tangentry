package tools

import (
	"encoding/json"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"

	"tangentry/internal/discovery"
	"tangentry/internal/downloads"
)

var mediaFormatID = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)

// parseDownloadMetadata maps yt-dlp's private JSON response to safe public
// choices. URLs, cookies and extractor-only fields are intentionally omitted.
func parseDownloadMetadata(raw []byte, rawURL string) (downloads.MediaInfo, error) {
	var data struct {
		Type      string  `json:"_type"`
		Title     string  `json:"title"`
		Uploader  string  `json:"uploader"`
		Channel   string  `json:"channel"`
		Thumbnail string  `json:"thumbnail"`
		Duration  float64 `json:"duration"`
		Extractor string  `json:"extractor_key"`
		Formats   []struct {
			ID          string  `json:"format_id"`
			Ext         string  `json:"ext"`
			Height      int     `json:"height"`
			Width       int     `json:"width"`
			FPS         float64 `json:"fps"`
			VideoCodec  string  `json:"vcodec"`
			AudioCodec  string  `json:"acodec"`
			ABR         float64 `json:"abr"`
			FileSize    int64   `json:"filesize"`
			Approximate int64   `json:"filesize_approx"`
			DRM         bool    `json:"has_drm"`
		} `json:"formats"`
		Subtitles map[string][]struct {
			Name string `json:"name"`
		} `json:"subtitles"`
		Automatic map[string][]struct {
			Name string `json:"name"`
		} `json:"automatic_captions"`
	}
	unavailable := func(message string) (downloads.MediaInfo, error) {
		return downloads.MediaInfo{}, &discovery.PlatformError{Message: message}
	}
	if json.Unmarshal(raw, &data) != nil {
		return unavailable("解析工具返回了无法识别的媒体信息。")
	}
	if data.Type == "playlist" || data.Type == "multi_video" {
		return unavailable("这个链接包含多条媒体，请打开其中一条视频或音频后再获取信息。")
	}
	if strings.TrimSpace(data.Title) == "" || len(data.Title) > 2000 || data.Duration < 0 {
		return unavailable("媒体信息不完整，请检查链接后重试。")
	}
	info := downloads.MediaInfo{URL: rawURL, Title: data.Title, Author: data.Uploader, Duration: data.Duration, Extractor: data.Extractor, Formats: []downloads.Format{}, Subtitles: []downloads.Subtitle{}}
	if info.Author == "" {
		info.Author = data.Channel
	}
	if u, err := url.Parse(data.Thumbnail); err == nil && u.User == nil && u.Hostname() != "" && (u.Scheme == "https" || u.Scheme == "http") {
		info.ThumbnailURL = u.String()
	}
	seen := map[string]bool{}
	for _, f := range data.Formats {
		if f.DRM || !mediaFormatID.MatchString(f.ID) || seen[f.ID] || f.VideoCodec == "none" && f.AudioCodec == "none" {
			continue
		}
		// Direct files and generic HLS often omit codec metadata.
		if f.VideoCodec == "" && f.AudioCodec == "" {
			if slices.Contains([]string{"m4a", "mp3", "aac", "opus", "ogg", "flac", "wav"}, strings.ToLower(f.Ext)) {
				f.VideoCodec, f.AudioCodec = "none", "未知编码"
			} else if slices.Contains([]string{"mp4", "mkv", "webm", "mov", "flv", "ts"}, strings.ToLower(f.Ext)) {
				f.VideoCodec, f.AudioCodec = "未知编码", "未知编码"
			} else {
				continue
			}
		}
		seen[f.ID] = true
		size := f.FileSize
		if size == 0 {
			size = f.Approximate
		}
		info.Formats = append(info.Formats, downloads.Format{ID: f.ID, Ext: f.Ext, Height: f.Height, Width: f.Width, FPS: f.FPS, VideoCodec: f.VideoCodec, AudioCodec: f.AudioCodec, AudioBitrate: f.ABR, FileSize: size, Approximate: f.FileSize == 0 && size > 0})
	}
	if len(info.Formats) == 0 {
		return unavailable("这个链接没有可下载的视频或音频格式，可能需要登录或不受当前工具支持。")
	}
	sort.SliceStable(info.Formats, func(i, j int) bool {
		a, b := info.Formats[i], info.Formats[j]
		if a.Height != b.Height {
			return a.Height > b.Height
		}
		if a.FPS != b.FPS {
			return a.FPS > b.FPS
		}
		return a.AudioBitrate > b.AudioBitrate
	})
	for index, tracks := range []map[string][]struct {
		Name string `json:"name"`
	}{data.Subtitles, data.Automatic} {
		for language, versions := range tracks {
			if !languagePattern.MatchString(language) || language == "live_chat" || language == "danmaku" || len(versions) == 0 {
				continue
			}
			label := versions[0].Name
			if label == "" {
				label = languageLabel(language)
			}
			info.Subtitles = append(info.Subtitles, downloads.Subtitle{Language: language, Label: label, Automatic: index == 1})
		}
	}
	sort.Slice(info.Subtitles, func(i, j int) bool {
		a, b := info.Subtitles[i], info.Subtitles[j]
		if a.Automatic != b.Automatic {
			return !a.Automatic
		}
		return a.Language < b.Language
	})
	return info, nil
}
