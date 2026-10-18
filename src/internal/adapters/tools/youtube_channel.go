package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"time"
	"unicode/utf8"
)

const maxYouTubeChannelEntries = 100

// YouTubeChannelFeed lists recent public videos without downloading media.
// The flat playlist extractor is deliberately kept separate from YouTubeChannel:
// channel resolution needs zero entries, while this path returns content items.
func (t *Toolkit) YouTubeChannelFeed(ctx context.Context, channelID string) (discovery.Feed, error) {
	if !discovery.YouTubeChannelID(channelID) {
		return discovery.Feed{}, discovery.ErrInvalidQuery
	}
	if !t.ytdlpReady() {
		return discovery.Feed{}, &discovery.PlatformError{Message: "YouTube 频道回退需要 yt-dlp，请检查工具设置。"}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	rawURL := discovery.YouTubeChannelURL(channelID) + "/videos"
	args := []string{"--ignore-config", "--no-cache-dir", "--simulate", "--flat-playlist", "--playlist-items", "1:30", "--extractor-args", "youtubetab:approximate_date", "--dump-single-json", "--socket-timeout", "10", "--retries", "0", "--", rawURL}
	cmd, err := t.ytdlpCommand(ctx, args...)
	if err != nil {
		return discovery.Feed{}, youtubeToolFailure(err, "")
	}
	stdout, stderr := &metadataBuffer{limit: 4 << 20}, &boundedBuffer{}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return discovery.Feed{}, ctx.Err()
		}
		return discovery.Feed{}, youtubeToolFailure(err, stderr.String())
	}
	if stdout.exceeded {
		return discovery.Feed{}, &discovery.PlatformError{Message: "YouTube yt-dlp 返回的数据过大，无法安全读取。"}
	}
	feed, err := ParseYouTubeChannelFeed(stdout.Bytes(), channelID)
	if err != nil {
		return discovery.Feed{}, err
	}
	return feed, nil
}

// ParseYouTubeChannelFeed maps yt-dlp's intentionally unstable flat-playlist
// JSON into Tangentry's stable content model. Unknown fields are ignored.
func ParseYouTubeChannelFeed(data []byte, channelID string) (discovery.Feed, error) {
	if !discovery.YouTubeChannelID(channelID) {
		return discovery.Feed{}, discovery.ErrInvalidQuery
	}
	var payload struct {
		ChannelID string            `json:"channel_id"`
		Channel   string            `json:"channel"`
		Uploader  string            `json:"uploader"`
		Title     string            `json:"title"`
		Entries   []json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return discovery.Feed{}, &discovery.PlatformError{Message: "YouTube yt-dlp 返回了无法识别的数据。"}
	}
	if payload.ChannelID != "" && payload.ChannelID != channelID {
		return discovery.Feed{}, &discovery.PlatformError{Message: "YouTube yt-dlp 返回的频道身份不一致。"}
	}
	author := strings.TrimSpace(payload.Channel)
	if author == "" {
		author = strings.TrimSpace(payload.Uploader)
	}
	if author == "" {
		author = strings.TrimSpace(payload.Title)
	}
	if !validYouTubeText(author, 200) {
		author = "YouTube 频道"
	}
	feed := discovery.Feed{Items: []content.Item{}, Page: 1, SourceTitle: author, Query: channelID}
	seen := make(map[string]bool, len(payload.Entries))
	for _, raw := range payload.Entries {
		if len(feed.Items) >= maxYouTubeChannelEntries {
			break
		}
		var entry struct {
			ID         string          `json:"id"`
			Title      string          `json:"title"`
			Channel    string          `json:"channel"`
			ChannelID  string          `json:"channel_id"`
			Uploader   string          `json:"uploader"`
			Duration   json.RawMessage `json:"duration"`
			Timestamp  json.RawMessage `json:"timestamp"`
			ViewCount  json.RawMessage `json:"view_count"`
			Thumbnail  string          `json:"thumbnail"`
			Thumbnails []struct {
				URL string `json:"url"`
			} `json:"thumbnails"`
		}
		if json.Unmarshal(raw, &entry) != nil || !validYouTubeVideoID(entry.ID) || seen[entry.ID] {
			continue
		}
		if entry.ChannelID != "" && entry.ChannelID != channelID {
			continue
		}
		title := strings.TrimSpace(entry.Title)
		if !validYouTubeText(title, 2000) {
			continue
		}
		itemAuthor := strings.TrimSpace(entry.Channel)
		if itemAuthor == "" {
			itemAuthor = strings.TrimSpace(entry.Uploader)
		}
		if !validYouTubeText(itemAuthor, 200) {
			itemAuthor = author
		}
		thumbnail := safeYouTubeThumbnail(entry.Thumbnail)
		if thumbnail == "" {
			for i := len(entry.Thumbnails) - 1; i >= 0; i-- {
				if thumbnail = safeYouTubeThumbnail(entry.Thumbnails[i].URL); thumbnail != "" {
					break
				}
			}
		}
		item := content.Item{ID: "youtube:" + entry.ID, Platform: "youtube", PlatformID: entry.ID, Kind: "video", Title: title, Author: itemAuthor, AuthorID: channelID, URL: "https://www.youtube.com/watch?v=" + entry.ID, ThumbnailURL: thumbnail, DurationSeconds: parseYouTubeInt(entry.Duration, 0, 24*60*60), PublishedAt: parseYouTubeInt64(entry.Timestamp, 0, 4102444800), ViewCount: parseYouTubeInt64(entry.ViewCount, 0, math.MaxInt64)}
		if content.Validate(item) != nil {
			continue
		}
		seen[entry.ID] = true
		feed.Items = append(feed.Items, item)
	}
	feed.Total = len(feed.Items)
	return feed, nil
}

func validYouTubeVideoID(id string) bool {
	if len(id) != 11 {
		return false
	}
	for _, r := range id {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

func validYouTubeText(value string, maxRunes int) bool {
	return value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= maxRunes && !strings.ContainsAny(value, "\x00\r\n")
}

func safeYouTubeThumbnail(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if !strings.HasSuffix(host, ".ytimg.com") && host != "ytimg.com" {
		return ""
	}
	return u.String()
}

func parseYouTubeInt(raw json.RawMessage, minValue, maxValue int) int {
	value := parseYouTubeInt64(raw, int64(minValue), int64(maxValue))
	return int(value)
}

func parseYouTubeInt64(raw json.RawMessage, minValue, maxValue int64) int64 {
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var number float64
	if json.Unmarshal(raw, &number) != nil {
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return 0
		}
		var err error
		number, err = strconv.ParseFloat(strings.TrimSpace(text), 64)
		if err != nil {
			return 0
		}
	}
	if math.IsNaN(number) || math.IsInf(number, 0) || number < float64(minValue) || number > float64(maxValue) {
		return 0
	}
	return int64(number)
}

func youtubeToolFailure(err error, stderr string) error {
	detail := redactYouTubeStderr(stderr)
	message := "YouTube yt-dlp 执行失败"
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		message += fmt.Sprintf("（退出码 %d", exit.ExitCode())
		if detail != "" {
			message += "，stderr: " + detail
		}
		message += "）"
	} else if detail != "" {
		message += "（stderr: " + detail + "）"
	} else {
		message += "。"
	}
	return &discovery.PlatformError{Message: message}
}

func redactYouTubeStderr(raw string) string {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "cookie") || strings.Contains(lower, "authorization") || strings.Contains(lower, "token") {
			line = "[已隐藏敏感信息]"
		}
		line = strings.Map(func(r rune) rune {
			if r == '\x00' || r == '\r' || r == '\n' {
				return -1
			}
			return r
		}, line)
		kept = append(kept, line)
	}
	result := strings.Join(kept, " | ")
	if len(result) > 800 {
		result = result[len(result)-800:]
	}
	return result
}

// YouTubeChannel resolves only public channel metadata. Zero playlist items
// prevents extraction of individual videos, subtitles, formats or media.
func (t *Toolkit) YouTubeChannel(ctx context.Context, raw string) (discovery.YouTubeChannel, error) {
	reference, err := discovery.ParseYouTubeChannel(raw)
	if err != nil {
		return discovery.YouTubeChannel{}, err
	}
	if reference.ID != "" {
		return discovery.YouTubeChannel{ID: reference.ID, URL: reference.URL}, nil
	}
	if !t.ytdlpReady() {
		return discovery.YouTubeChannel{}, &discovery.PlatformError{Message: "频道名称解析需要 yt-dlp；请检查工具设置，或输入以 UC 开头的频道 ID。"}
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	args := []string{"--ignore-config", "--no-cache-dir", "--simulate", "--skip-download", "--flat-playlist", "--playlist-items", "0", "--dump-single-json", "--socket-timeout", "10", "--retries", "0", "--", reference.URL}
	cmd, err := t.ytdlpCommand(ctx, args...)
	if err != nil {
		return discovery.YouTubeChannel{}, &discovery.PlatformError{Message: "yt-dlp 未能下载，请检查网络后重试。"}
	}
	stdout, stderr := &metadataBuffer{}, &boundedBuffer{}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err = cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return discovery.YouTubeChannel{}, ctx.Err()
		}
		return discovery.YouTubeChannel{}, &discovery.PlatformError{Message: "YouTube 频道暂时无法解析，请检查频道地址与网络，或输入 UC 频道 ID。"}
	}
	var data struct {
		ChannelID  string            `json:"channel_id"`
		Channel    string            `json:"channel"`
		Title      string            `json:"title"`
		ChannelURL string            `json:"channel_url"`
		Entries    []json.RawMessage `json:"entries"`
	}
	if stdout.exceeded || json.Unmarshal(stdout.Bytes(), &data) != nil || !discovery.YouTubeChannelID(data.ChannelID) || len(data.Entries) != 0 {
		return discovery.YouTubeChannel{}, &discovery.PlatformError{Message: "频道解析没有返回可信的频道身份，请使用 UC 频道 ID。"}
	}
	if data.ChannelURL != "" {
		actual, err := discovery.ParseYouTubeChannel(data.ChannelURL)
		if err != nil || actual.ID != data.ChannelID {
			return discovery.YouTubeChannel{}, &discovery.PlatformError{Message: "频道解析结果不一致，请稍后重试。"}
		}
	}
	title := data.Channel
	if strings.TrimSpace(title) == "" {
		title = data.Title
	}
	title = strings.TrimSpace(title)
	if !utf8.ValidString(title) || utf8.RuneCountInString(title) > 200 || strings.ContainsAny(title, "\x00\r\n") {
		return discovery.YouTubeChannel{}, &discovery.PlatformError{Message: "频道解析结果缺少可用的名称。"}
	}
	// Use only Tangentry's own channel fields. Never forward the extractor payload.

	return discovery.YouTubeChannel{ID: data.ChannelID, Title: title, URL: discovery.YouTubeChannelURL(data.ChannelID)}, nil
}
