package tools

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"

	"tangentry/internal/netpolicy"
	"tangentry/internal/transcription"
)

const maxSpeechAudio = 512 << 20

func speechMediaURL(platform, raw string) bool {
	if len(raw) > 8192 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Opaque != "" || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if platform == "douyin" {
		return strings.HasSuffix(host, ".douyinvod.com") || strings.HasSuffix(host, ".douyin.com")
	}
	if platform == "xiaohongshu" {
		return strings.HasSuffix(host, ".xhscdn.com")
	}
	return platform == "youtube" && strings.HasSuffix(host, ".googlevideo.com") || platform == "bilibili" && (strings.HasSuffix(host, ".bilivideo.com") || strings.HasSuffix(host, ".bilivideo.cn"))
}

func (t *Transcriber) fetchSpeechAudio(ctx context.Context, audio transcription.Audio, path string) error {
	if audio.Platform != "" {
		return t.fetchPlatformAudio(ctx, audio, path)
	}
	// Public feed URLs retain the DNS/redirect checks of the safe HTTP client.
	raw, err := netpolicy.URL(audio.URL)
	if err != nil {
		return transcription.ErrInvalid
	}
	var failure error
	for attempt := 0; attempt < 2; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		failure = t.fetchPublicAudio(ctx, raw, audio.Referer, path)
		if failure == nil {
			return nil
		}
		var reason *transcription.Failure
		if !errors.As(failure, &reason) || (reason.Code != "audio_connection" && reason.Code != "audio_interrupted") {
			return failure
		}
	}
	return failure
}

func (t *Transcriber) fetchPlatformAudio(ctx context.Context, audio transcription.Audio, path string) error {
	if !speechMediaURL(audio.Platform, audio.URL) {
		return transcription.ErrInvalid
	}
	binary := executable(t.YTDLP, "yt-dlp")
	if t.ResolveYTDLP != nil {
		resolved, err := t.ResolveYTDLP(ctx)
		if err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		binary = resolved
	}
	if binary == "" {
		return transcription.Failed("ytdlp_not_ready", "视频音轨下载需要 yt-dlp，请先在工具设置检查下载工具。")
	}
	sources := []string{audio.URL}
	for _, raw := range audio.Backups {
		if raw != audio.URL && speechMediaURL(audio.Platform, raw) {
			sources = append(sources, raw)
			break
		}
	}
	var failure error
	for i, raw := range sources {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if i > 0 {
			// A backup may use a different representation: never append its bytes
			// to the primary CDN's partial file.
			for _, name := range []string{path, path + ".part", path + ".ytdl"} {
				if err := os.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
					return storageFailure(err)
				}
			}
		}
		args := []string{"--ignore-config", "--no-plugin-dirs", "--no-cache-dir", "--no-playlist", "--no-progress", "--no-warnings", "--force-generic-extractor", "--socket-timeout", "15", "--retries", "2", "--fragment-retries", "2", "--concurrent-fragments", "1", "--http-chunk-size", "2M", "--max-filesize", "512M", "--no-mtime", "--output", path, "--user-agent", "Mozilla/5.0"}
		if audio.Referer != "" {
			args = append(args, "--referer", audio.Referer)
		}
		args = append(args, "--", raw)
		cmd := command(ctx, binary, args...)
		cmd.Env = toolEnvironment()
		warnings := &boundedBuffer{}
		cmd.Stdout, cmd.Stderr = warnings, warnings
		if err := cmd.Run(); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			text := strings.ToLower(warnings.String())
			if strings.Contains(text, "no space left") {
				return storageFailure(syscall.ENOSPC)
			}
			failure = transcription.Failed("audio_download", "yt-dlp 未能下载完整音轨，请检查网络，或重新获取媒体连接后重试。")
			continue
		}
		info, err := os.Lstat(path)
		if err != nil {
			failure = transcription.Failed("audio_download", "yt-dlp 没有生成音轨文件，媒体可能超过 512 MB 或已不可访问。")
			continue
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			return transcription.Failed("audio_empty", "音轨下载结果为空或无效，请确认内容可播放后重试。")
		}
		if info.Size() > maxSpeechAudio {
			return transcription.Failed("audio_too_large", "媒体源超过 512 MB，请使用更小的音频源。")
		}
		return nil
	}
	return failure
}

func (t *Transcriber) fetchPublicAudio(ctx context.Context, raw, referer, path string) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept-Encoding", "identity")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	client := *t.HTTP
	client.Timeout = 0
	res, err := client.Do(req)
	if err != nil {
		return transcription.Failed("audio_connection", "无法连接音频来源，请检查网络后重试。")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return transcription.Failed("audio_http", "音频来源拒绝了读取请求，媒体连接可能已失效，请重试。")
	}
	if res.ContentLength > maxSpeechAudio {
		return transcription.Failed("audio_too_large", "媒体源超过 512 MB，请使用更小的音频源。")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return storageFailure(err)
	}
	n, copyErr := io.Copy(file, io.LimitReader(res.Body, maxSpeechAudio+1))
	closeErr := file.Close()
	if copyErr != nil {
		if errors.Is(copyErr, syscall.ENOSPC) || errors.Is(copyErr, os.ErrPermission) {
			return storageFailure(copyErr)
		}
		return transcription.Failed("audio_interrupted", "音频下载中断，未取得完整媒体，请检查网络后重试。")
	}
	if closeErr != nil {
		return storageFailure(closeErr)
	}
	if n > maxSpeechAudio {
		return transcription.Failed("audio_too_large", "媒体源超过 512 MB，请使用更小的音频源。")
	}
	if n == 0 {
		return transcription.Failed("audio_empty", "音频来源返回空文件，请确认内容可播放后重试。")
	}
	if res.ContentLength >= 0 && n != res.ContentLength {
		return transcription.Failed("audio_interrupted", "音频下载中断，未取得完整媒体，请检查网络后重试。")
	}
	return nil
}
