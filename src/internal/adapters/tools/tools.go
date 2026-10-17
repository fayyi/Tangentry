// Package tools contains CLI-specific behavior. Business code never builds shell commands.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"tangentry/internal/adapters/platforms/youtube"
	"tangentry/internal/config"
	"tangentry/internal/credentials"
	"tangentry/internal/downloads"
)

type Toolkit struct {
	youtubeAvatarMu    sync.Mutex
	youtubeAvatars     map[string]youtubeAvatarEntry
	youtubeAvatarSlots chan struct{}
	youtubePages       youtubePageState
	youtubeSessions    map[string]*youtubePageState
	youtubeMu          sync.Mutex
	youtubeHome        func(context.Context, string, json.RawMessage) youtube.HomeResult
	credentials        *credentials.Store
	config             config.Config
	status             downloads.ToolReport
	// ytdlp and deno are downloaded into the app data directory on first use
	// when the desktop runtime configures a tools directory.
	ytdlp *managedTool
	deno  *managedTool
}

func New(c config.Config) *Toolkit {
	t := &Toolkit{config: c, youtubeAvatars: map[string]youtubeAvatarEntry{}, youtubeAvatarSlots: make(chan struct{}, 2), youtubeHome: youtube.NewHome().Fetch}
	t.config.FFmpegPath = executable(c.FFmpegPath, "ffmpeg")
	ytdlp := downloads.ToolStatus{Name: "yt-dlp"}
	if c.YTDLPPath == "" && c.ToolsDir != "" {
		t.ytdlp = newManagedTool("yt-dlp", c.ToolsDir, pinnedYTDLP)
		t.deno = newManagedTool("deno", c.ToolsDir, pinnedDeno)
	} else {
		t.config.YTDLPPath = executable(c.YTDLPPath, "yt-dlp")
		ytdlp = probe("yt-dlp", t.config.YTDLPPath, "--version")
	}
	t.status = downloads.ToolReport{DownloadDir: c.DownloadDir, CookieConfigured: c.CookieFile != "", Tools: []downloads.ToolStatus{ytdlp, probe("FFmpeg", t.config.FFmpegPath, "-version")}}
	return t
}

// ytdlpReady reports whether yt-dlp is installed or will be downloaded on use.
func (t *Toolkit) ytdlpReady() bool { return t.ytdlp != nil || t.status.Tools[0].Available }

// YTDLP returns the yt-dlp executable, downloading the pinned release first
// when the app manages it.
func (t *Toolkit) YTDLP(ctx context.Context) (string, error) {
	if t.ytdlp == nil {
		if t.config.YTDLPPath == "" {
			return "", downloads.ErrTools
		}
		return t.config.YTDLPPath, nil
	}
	return t.ytdlp.ensure(ctx)
}

// ytdlpCommand prepares a yt-dlp invocation. Managed installs also receive a
// JavaScript runtime for YouTube extraction: a system deno or node when one
// exists, otherwise the pinned Deno downloaded on first use. A failed runtime
// download degrades extraction instead of failing it.
func (t *Toolkit) ytdlpCommand(ctx context.Context, args ...string) (*exec.Cmd, error) {
	binary, err := t.YTDLP(ctx)
	if err != nil {
		return nil, err
	}
	if t.ytdlp != nil && needsJavaScript(args) {
		if runtime := t.javaScriptRuntime(ctx); runtime != "" {
			args = append([]string{"--js-runtimes", runtime}, args...)
		}
	}
	cmd := command(ctx, binary, args...)
	cmd.Env = toolEnvironment()
	return cmd, nil
}

func needsJavaScript(args []string) bool {
	youtube := false
	for _, arg := range args {
		switch {
		case arg == "--flat-playlist", arg == "--force-generic-extractor":
			return false
		case strings.Contains(arg, "youtube.com/"), strings.Contains(arg, "youtu.be/"):
			youtube = true
		}
	}
	return youtube
}

// UpdateYTDLP replaces the managed yt-dlp with the latest verified release in
// the background; Status reports its progress.
func (t *Toolkit) UpdateYTDLP() error {
	if t.ytdlp == nil {
		return downloads.ErrTools
	}
	if err := t.ytdlp.update(func(ctx context.Context) (managedRelease, error) { return latestYTDLP(ctx, t.ytdlp.client) }); err != nil {
		return downloads.ErrToolBusy
	}
	return nil
}

func (t *Toolkit) Ready() bool { return t.ytdlpReady() && t.status.Tools[1].Available }
func (t *Toolkit) Status() downloads.ToolReport {
	report := t.status
	report.Tools = append([]downloads.ToolStatus{}, t.status.Tools...)
	if t.ytdlp != nil {
		state := t.ytdlp.snapshot()
		report.Tools[0] = downloads.ToolStatus{Name: "yt-dlp", Available: true, Version: state.Version, Managed: true, Installed: state.Installed,
			Downloading: state.Downloading, Received: state.Received, Total: state.Total, Error: state.Error}
	}
	report.DownloadReady = t.Ready()
	report.YouTubeRecommendation = t.YouTubeRecommendationStatus()
	if t.credentials != nil {
		for _, state := range t.credentials.States() {
			if state.Platform == "bilibili" {
				report.CookieConfigured = state.Configured
			}
		}
	}
	return report
}
func (t *Toolkit) SetCredentials(value *credentials.Store) { t.credentials = value }

// Invocations snapshot their own platform jar; no shared file is passed to yt-dlp.
func (t *Toolkit) cookieArguments(rawURL string) ([]string, func(), error) {
	platform := credentials.PlatformForURL(rawURL)
	if platform == "" {
		return nil, func() {}, nil
	}
	if t.credentials != nil {
		file, clean, err := t.credentials.Snapshot(platform)
		if err != nil {
			return nil, clean, err
		}
		if file != "" {
			return []string{"--cookies", file}, clean, nil
		}
		return nil, clean, nil
	}
	if platform == "bilibili" && t.config.CookieFile != "" {
		return []string{"--cookies", t.config.CookieFile}, func() {}, nil
	}
	return nil, func() {}, nil
}

func (t *Toolkit) arguments(rawURL, outputDir string, maxHeight int) []string {
	return t.mediaArguments(rawURL, outputDir, maxHeight, downloads.Options{})
}

func (t *Toolkit) Download(ctx context.Context, rawURL, outputDir string, maxHeight int) (string, error) {
	result, err := t.DownloadMedia(ctx, rawURL, outputDir, maxHeight, downloads.Options{})
	return result.FileName, err
}

func classify(raw string) error {
	text := strings.ToLower(raw)
	switch {
	case strings.Contains(text, "412"), strings.Contains(text, "403"), strings.Contains(text, "429"), strings.Contains(text, "-352"), strings.Contains(text, "risk control"):
		return errors.New("平台限制了下载请求，请稍后重试；登录态或网络环境可能影响访问。")
	case strings.Contains(text, "login"), strings.Contains(text, "sign in"), strings.Contains(text, "cookies"), strings.Contains(text, "premium"):
		return errors.New("此视频需要有效登录态或账号权限，请检查登录配置。")
	case strings.Contains(text, "requested format"), strings.Contains(text, "no video formats"):
		return errors.New("这段视频没有可用的对应画质，请尝试其他画质。")
	case strings.Contains(text, "no space left"):
		return errors.New("下载磁盘空间不足。")
	default:
		return fmt.Errorf("下载未完成，请检查视频是否可访问、网络和工具配置后重试。")
	}
}

// Subprocess output can be large and sensitive. Retain only a bounded tail in memory.
type boundedBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if n > 16384 {
		p = p[n-16384:]
	}
	b.data = append(b.data, p...)
	if len(b.data) > 16384 {
		b.data = b.data[len(b.data)-16384:]
	}
	return n, nil
}
func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(bytes.Clone(b.data))
}
