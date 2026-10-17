package tools

import (
	"context"
	"encoding/json"
	"strings"
	"tangentry/internal/discovery"
	"time"
	"unicode/utf8"
)

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
