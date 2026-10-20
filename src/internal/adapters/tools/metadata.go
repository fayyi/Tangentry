package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"time"

	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"tangentry/internal/playback"
)

// Video runs only metadata extraction. It never downloads media, subtitles, or models.
func (t *Toolkit) Video(ctx context.Context, bvid string) (content.Item, error) {
	kind, id, err := discovery.ParseQuery(bvid)
	if err != nil || kind != "video" {
		return content.Item{}, discovery.ErrInvalidQuery
	}
	return t.extractVideo(ctx, "bilibili", id, "https://www.bilibili.com/video/"+id+"/")
}
func (t *Toolkit) YouTube(ctx context.Context, raw string) (content.Item, error) {
	id, url, err := discovery.ParseYouTube(raw)
	if err != nil {
		return content.Item{}, err
	}
	return t.extractVideo(ctx, "youtube", id, url)
}

// Playback resolves a single HTTPS audio/video representation for online
// playback fallback. It never downloads a file and never returns separate DASH
// tracks, because the browser playback service expects one combined stream.
func (t *Toolkit) Playback(ctx context.Context, bvid string) (playback.Source, error) {
	kind, id, err := discovery.ParseQuery(bvid)
	if err != nil || kind != "video" {
		return playback.Source{}, discovery.ErrInvalidQuery
	}
	if !t.ytdlpReady() {
		return playback.Source{}, discovery.ErrUnavailable
	}
	rawURL := "https://www.bilibili.com/video/" + id + "/"
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cookieArgs, clean, err := t.cookieArguments(rawURL)
	if err != nil {
		return playback.Source{}, discovery.ErrUnavailable
	}
	defer clean()
	args := []string{"--ignore-config", "--no-plugin-dirs", "--no-playlist", "--no-cache-dir", "--no-warnings", "--skip-download", "--dump-single-json", "--format", "b[protocol=https][height=1080][vcodec!=none][acodec!=none]/b[protocol=https][height<=1080][vcodec!=none][acodec!=none]/b[protocol=https][vcodec!=none][acodec!=none]", "--socket-timeout", "12", "--retries", "1"}
	args = append(args, cookieArgs...)
	args = append(args, "--", rawURL)
	cmd, err := t.ytdlpCommand(ctx, args...)
	if err != nil {
		return playback.Source{}, discovery.ErrUnavailable
	}
	stdout, stderr := &metadataBuffer{}, &boundedBuffer{}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil || stdout.exceeded {
		if ctx.Err() != nil {
			return playback.Source{}, ctx.Err()
		}
		return playback.Source{}, discovery.ErrUnavailable
	}
	var data struct {
		ID       string  `json:"id"`
		URL      string  `json:"url"`
		Protocol string  `json:"protocol"`
		Format   string  `json:"format"`
		Height   int     `json:"height"`
		Duration float64 `json:"duration"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &data); err != nil || data.ID != id || data.URL == "" || data.Protocol != "https" || data.Format == "" || math.IsNaN(data.Duration) || math.IsInf(data.Duration, 0) || data.Duration < 0 || data.Duration > 21600 || !playback.ValidSource(playback.Source{URL: data.URL}) {
		return playback.Source{}, playback.ErrUnavailable
	}
	return playback.Source{URL: data.URL, Quality: data.Height, Duration: int(data.Duration)}, nil
}
func (t *Toolkit) Douyin(ctx context.Context, raw string) (content.Item, error) {
	ref, err := discovery.ParseDouyin(raw)
	if err != nil || ref.ID == "" {
		return content.Item{}, discovery.ErrInvalidQuery
	}
	return t.extractVideo(ctx, "douyin", ref.ID, ref.URL)
}

// DouyinPlayback resolves one progressive MP4 representation. Separate DASH
// tracks are intentionally excluded because temporary playback needs a single
// seekable body for the native player.
func (t *Toolkit) DouyinPlayback(ctx context.Context, raw string) (playback.Source, error) {
	trace := playback.DiagnosticsFromContext(ctx)
	ref, err := discovery.ParseDouyin(raw)
	if err != nil || ref.ID == "" {
		return playback.Source{}, discovery.ErrInvalidQuery
	}
	if !t.ytdlpReady() {
		recordYTDLPStage(ctx, playback.OutcomeException, "tool_unavailable", nil, nil, false, nil)
		return playback.Source{}, playback.WrapDiagnostic(&discovery.PlatformError{Message: "抖音视频解析工具尚未准备好，请在工具设置中检查 yt-dlp。"}, trace, playback.DiagnosticSummary(traceStages(trace)))
	}
	version := t.ytdlpVersion()
	cookieConfigured := t.douyinCookieConfigured()
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	cookieArgs, clean, err := t.cookieArguments(ref.URL)
	if err != nil {
		recordYTDLPStage(ctx, playback.OutcomeException, "cookie_read_error", nil, nil, cookieConfigured, version)
		return playback.Source{}, playback.WrapDiagnostic(&discovery.PlatformError{Message: "抖音 Cookie 读取失败，请在工具设置 → 抖音 Cookie 检查后重试。"}, trace, playback.DiagnosticSummary(traceStages(trace)))
	}
	defer clean()
	args := []string{"--ignore-config", "--no-playlist", "--no-cache-dir", "--no-warnings", "--skip-download", "--dump-single-json", "--format", "b[ext=mp4][height<=1080][vcodec!=none][acodec!=none]/b[ext=mp4][height<=1080][vcodec!=none][acodec!=none]/b[ext=mp4][vcodec!=none][acodec!=none]", "--socket-timeout", "12", "--retries", "1"}
	args = append(args, cookieArgs...)
	args = append(args, "--", ref.URL)
	cmd, err := t.ytdlpCommand(ctx, args...)
	if err != nil {
		recordYTDLPStage(ctx, playback.OutcomeException, "command_error", nil, nil, cookieConfigured, version)
		return playback.Source{}, playback.WrapDiagnostic(&discovery.PlatformError{Message: "抖音视频解析工具不可用，请在工具设置中检查 yt-dlp。"}, trace, playback.DiagnosticSummary(traceStages(trace)))
	}
	if current := t.ytdlpVersion(); current != nil {
		version = current
	}
	stdout, stderr, diagnosticStderr := &metadataBuffer{}, &boundedBuffer{}, &firstErrorLineBuffer{}
	cmd.Stdout, cmd.Stderr = stdout, io.MultiWriter(stderr, diagnosticStderr)
	if err := cmd.Run(); err != nil || stdout.exceeded {
		var exitCode *int
		if cmd.ProcessState != nil {
			value := cmd.ProcessState.ExitCode()
			exitCode = &value
		}
		outcome, reason := playback.OutcomeException, "command_failed"
		if ctx.Err() != nil {
			outcome, reason = playback.OutcomeTimeout, "timeout"
			if errors.Is(ctx.Err(), context.Canceled) {
				outcome, reason = playback.OutcomeException, "cancelled"
			}
		} else if stdout.exceeded {
			outcome, reason = playback.OutcomeTooLarge, "stdout_too_large"
		}
		stderrValue := diagnosticStderr.String()
		recordYTDLPStage(ctx, outcome, reason, exitCode, &stderrValue, cookieConfigured, version)
		message := classifyDouyin(stderr.String(), len(cookieArgs) > 0).Error()
		if trace != nil {
			message = playback.DiagnosticSummary(trace.Snapshot().Stages)
		}
		return playback.Source{}, playback.WrapDiagnostic(&discovery.PlatformError{Message: message}, trace, message)
	}
	var data struct {
		ID       string  `json:"id"`
		URL      string  `json:"url"`
		Protocol string  `json:"protocol"`
		Ext      string  `json:"ext"`
		Format   string  `json:"format"`
		VCodec   string  `json:"vcodec"`
		ACodec   string  `json:"acodec"`
		Height   int     `json:"height"`
		Duration float64 `json:"duration"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &data); err != nil || data.ID != ref.ID || data.URL == "" || data.Protocol != "https" || data.Ext != "mp4" || data.Height > 1080 || data.VCodec == "" || data.ACodec == "" || math.IsNaN(data.Duration) || math.IsInf(data.Duration, 0) || data.Duration < 0 || data.Duration > 1800 || !playback.ValidSource(playback.Source{URL: data.URL, Platform: "douyin"}) {
		reason := "format_invalid"
		if err := json.Unmarshal(stdout.Bytes(), &data); err != nil {
			reason = "json_invalid"
		}
		recordYTDLPStage(ctx, playback.OutcomeNoURL, reason, nil, nil, cookieConfigured, version)
		if trace != nil {
			protocol, extension := data.Protocol, data.Ext
			var videoCodec, audioCodec *string
			if data.VCodec != "" {
				videoCodec = &data.VCodec
			}
			if data.ACodec != "" {
				audioCodec = &data.ACodec
			}
			duration := int(data.Duration)
			var allowed *bool
			if data.URL != "" {
				value := playback.ValidSource(playback.Source{URL: data.URL, Platform: "douyin"})
				allowed = &value
			}
			trace.Add(playback.DiagnosticStage{Stage: "source_validate", Outcome: playback.OutcomeNoURL, Reason: &reason, Protocol: &protocol, Extension: &extension, VideoCodec: videoCodec, AudioCodec: audioCodec, DurationSeconds: &duration, CDNAllowed: allowed})
		}
		message := "抖音没有返回可兼容播放的 MP4 视频，请下载视频或打开原内容。"
		if trace != nil {
			message = playback.DiagnosticSummary(trace.Snapshot().Stages)
		}
		return playback.Source{}, playback.WrapDiagnostic(&discovery.PlatformError{Message: message}, trace, message)
	}
	if trace != nil {
		reason := "source_valid"
		protocol, extension, videoCodec, audioCodec := data.Protocol, data.Ext, data.VCodec, data.ACodec
		duration := int(data.Duration)
		allowed := true
		trace.Add(playback.DiagnosticStage{Stage: "source_validate", Outcome: playback.OutcomeOK, Reason: &reason, Protocol: &protocol, Extension: &extension, VideoCodec: &videoCodec, AudioCodec: &audioCodec, DurationSeconds: &duration, CDNAllowed: &allowed})
	}
	recordYTDLPStage(ctx, playback.OutcomeOK, "resolver_success", nil, nil, cookieConfigured, version)
	return playback.Source{URL: data.URL, Quality: data.Height, Duration: int(data.Duration), Protocol: data.Protocol, Extension: data.Ext, VideoCodec: data.VCodec, AudioCodec: data.ACodec, Platform: "douyin", Referer: ref.URL}, nil
}

func (t *Toolkit) ytdlpVersion() *string {
	if t.ytdlp != nil {
		version := t.ytdlp.snapshot().Version
		if version == "" {
			return nil
		}
		return &version
	}
	if len(t.status.Tools) == 0 || t.status.Tools[0].Version == "" {
		return nil
	}
	version := t.status.Tools[0].Version
	return &version
}

func (t *Toolkit) douyinCookieConfigured() bool {
	if t.credentials != nil {
		for _, state := range t.credentials.States() {
			if state.Platform == "douyin" {
				return state.Configured
			}
		}
	}
	return false
}

type firstErrorLineBuffer struct {
	line  []byte
	value string
}

func (buffer *firstErrorLineBuffer) Write(value []byte) (int, error) {
	originalLength := len(value)
	if buffer.value != "" {
		return originalLength, nil
	}
	for _, char := range value {
		if char == '\n' {
			if strings.Contains(strings.ToUpper(string(buffer.line)), "ERROR") {
				buffer.value = string(buffer.line)
				buffer.line = nil
				break
			}
			buffer.line = buffer.line[:0]
			continue
		}
		if len(buffer.line) < 4096 {
			buffer.line = append(buffer.line, char)
		}
	}
	return originalLength, nil
}

func (buffer *firstErrorLineBuffer) String() string {
	if buffer.value != "" {
		return buffer.value
	}
	if strings.Contains(strings.ToUpper(string(buffer.line)), "ERROR") {
		return string(buffer.line)
	}
	return ""
}

func recordYTDLPStage(ctx context.Context, outcome playback.DiagnosticOutcome, reason string, exitCode *int, stderr *string, cookieConfigured bool, version *string) {
	trace := playback.DiagnosticsFromContext(ctx)
	if trace == nil {
		return
	}
	configured := cookieConfigured
	reasonValue := reason
	trace.Add(playback.DiagnosticStage{Stage: "ytdlp", Outcome: outcome, Reason: &reasonValue, YTDLPVersion: version, YTDLPExitCode: exitCode, YTDLPStderrFirstErrorRedacted: stderr, CookieConfigured: &configured})
}

func traceStages(trace *playback.DiagnosticTrace) []playback.DiagnosticStage {
	if trace == nil {
		return nil
	}
	return trace.Snapshot().Stages
}

func (t *Toolkit) extractVideo(ctx context.Context, platform, id, url string) (content.Item, error) {
	if !t.ytdlpReady() {
		return content.Item{}, discovery.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	args := []string{"--ignore-config", "--no-playlist", "--no-cache-dir", "--no-warnings", "--skip-download", "--dump-single-json", "--socket-timeout", "10", "--retries", "0"}
	cookieArgs, clean, err := t.cookieArguments(url)
	if err != nil {
		return content.Item{}, err
	}
	defer clean()
	args = append(args, cookieArgs...)
	args = append(args, "--", url)
	cmd, err := t.ytdlpCommand(ctx, args...)
	if err != nil {
		return content.Item{}, discovery.ErrUnavailable
	}
	stdout, stderr := &metadataBuffer{}, &boundedBuffer{}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return content.Item{}, ctx.Err()
		}
		if platform == "douyin" {
			cookieArgs, _, _ := t.cookieArguments(url)
			return content.Item{}, &discovery.PlatformError{Message: classifyDouyin(stderr.String(), len(cookieArgs) > 0).Error()}
		}
		return content.Item{}, &discovery.PlatformError{Message: classify(stderr.String()).Error()}
	}
	if stdout.exceeded {
		return content.Item{}, errors.New("media metadata exceeded limit")
	}
	var data struct {
		ID          string  `json:"id"`
		Title       string  `json:"title"`
		Uploader    string  `json:"uploader"`
		UploaderID  string  `json:"uploader_id"`
		Channel     string  `json:"channel"`
		ChannelID   string  `json:"channel_id"`
		Thumbnail   string  `json:"thumbnail"`
		Description string  `json:"description"`
		Duration    float64 `json:"duration"`
		Timestamp   int64   `json:"timestamp"`
		UploadDate  string  `json:"upload_date"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &data); err != nil {
		return content.Item{}, &discovery.PlatformError{Message: "视频解析工具返回了无法识别的数据。"}
	}
	if data.ID != id && !(platform == "bilibili" && data.ID == id+"_p1") {
		return content.Item{}, &discovery.PlatformError{Message: "视频解析结果与请求不一致。"}
	}
	if math.IsNaN(data.Duration) || math.IsInf(data.Duration, 0) || data.Duration < 0 || data.Duration >= float64(^uint(0)>>1) {
		return content.Item{}, &discovery.PlatformError{Message: "视频时长无效。"}
	}
	if data.Timestamp == 0 {
		if date, err := time.Parse("20060102", data.UploadDate); err == nil {
			data.Timestamp = date.Unix()
		}
	}
	item := content.Item{ID: platform + ":" + id, Platform: platform, PlatformID: id, Kind: "video", Title: data.Title, Author: data.Uploader, AuthorID: data.UploaderID, URL: url, ThumbnailURL: data.Thumbnail, Description: data.Description, DurationSeconds: int(data.Duration), PublishedAt: data.Timestamp}
	if platform == "youtube" {
		if strings.TrimSpace(item.Author) == "" {
			item.Author = strings.TrimSpace(data.Channel)
		}
		// Extractor uploader IDs are handles, not stable YouTube channel identities.
		item.AuthorID = ""
		if discovery.YouTubeChannelID(data.ChannelID) {
			item.AuthorID = data.ChannelID
		} else if discovery.YouTubeChannelID(data.UploaderID) {
			item.AuthorID = data.UploaderID
		}
	}
	if err := content.Validate(item); err != nil {
		return content.Item{}, &discovery.PlatformError{Message: "视频解析结果缺少必要的信息。"}
	}
	if platform == "youtube" {
		item.AuthorAvatarURL = t.youtubeAvatar(ctx, item.AuthorID)
	}
	return item, nil
}

type metadataBuffer struct {
	bytes.Buffer
	exceeded bool
	limit    int
}

func (b *metadataBuffer) Write(p []byte) (int, error) {
	n := len(p)
	limit := b.limit
	if limit == 0 {
		limit = 4 << 20
	}
	remaining := max(0, limit-b.Len())
	if n > remaining {
		p = p[:remaining]
		b.exceeded = true
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}
