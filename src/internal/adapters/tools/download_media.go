package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"tangentry/internal/discovery"
	"tangentry/internal/downloads"
)

func (t *Toolkit) Inspect(ctx context.Context, rawURL string) (downloads.MediaInfo, error) {
	if !t.ytdlpReady() {
		return downloads.MediaInfo{}, downloads.ErrTools
	}
	cookies, clean, err := t.cookieArguments(rawURL)
	if err != nil {
		return downloads.MediaInfo{}, downloads.ErrTools
	}
	defer clean()
	args := []string{"--ignore-config", "--no-plugin-dirs", "--no-playlist", "--playlist-items", "1", "--no-cache-dir", "--simulate", "--skip-download", "--dump-single-json", "--socket-timeout", "12", "--retries", "0"}
	args = append(args, cookies...)
	args = append(args, "--", rawURL)
	cmd, err := t.ytdlpCommand(ctx, args...)
	if err != nil {
		return downloads.MediaInfo{}, t.toolDownloadError(ctx, err)
	}
	stdout, stderr := &metadataBuffer{limit: 8 << 20}, &boundedBuffer{}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return downloads.MediaInfo{}, ctx.Err()
		}
		return downloads.MediaInfo{}, &discovery.PlatformError{Message: classify(stderr.String()).Error()}
	}
	if stdout.exceeded {
		return downloads.MediaInfo{}, &discovery.PlatformError{Message: "媒体信息过大，请使用单条视频或音频链接。"}
	}
	return parseDownloadMetadata(stdout.Bytes(), rawURL)
}

func (t *Toolkit) mediaArguments(rawURL, directory string, height int, options downloads.Options) []string {
	container := options.Container
	if container == "" {
		container = "mp4"
	}
	video := "bv*"
	combined := "b"
	if height > 0 {
		filter := "[height<=?" + strconv.Itoa(height) + "]"
		video += filter
		combined += filter
	}
	if options.VideoOnly {
		video = strings.Replace(video, "bv*", "bv", 1)
	}
	if options.VideoFormatID != "" {
		video, combined = options.VideoFormatID, options.VideoFormatID
	}
	audio := "ba"
	if options.AudioFormatID != "" {
		audio = options.AudioFormatID
	}
	selector := video + "+" + audio + "/" + combined
	if options.VideoOnly {
		selector = video + "/" + combined
	}
	if options.Mode == "audio" {
		selector = audio + "/b"
	}
	args := []string{"--ignore-config", "--no-plugin-dirs", "--no-playlist", "--playlist-items", "1", "--no-cache-dir", "--no-progress", "--no-warnings", "--socket-timeout", "20", "--retries", "1", "--fragment-retries", "1", "--concurrent-fragments", "1", "--no-overwrites", "--windows-filenames", "--ffmpeg-location", t.config.FFmpegPath, "--format", selector, "--paths", directory, "--output", "%(title).180B.%(ext)s", "--print", "after_move:filepath"}
	if options.Mode == "audio" {
		format := options.AudioFormat
		if format == "" {
			format = "best"
		}
		args = append(args, "--extract-audio", "--audio-format", format, "--audio-quality", "0")
	} else {
		args = append(args, "--merge-output-format", container, "--remux-video", container)
		if container == "mp4" {
			args = append(args, "--format-sort", "res,fps,vcodec:h264,acodec:aac")
		}
	}
	if len(options.SubtitleLanguages) > 0 {
		languages := make([]string, 0, len(options.SubtitleLanguages))
		for _, language := range options.SubtitleLanguages {
			languages = append(languages, "^"+regexp.QuoteMeta(language)+"$")
		}
		args = append(args, "--write-subs", "--sub-langs", strings.Join(languages, ","), "--sub-format", "srt/vtt/best")
		if options.AutomaticSubtitles {
			args = append(args, "--write-auto-subs")
		}
	}
	return append(args, "--", rawURL)
}

func (t *Toolkit) DownloadMedia(ctx context.Context, rawURL, directory string, height int, options downloads.Options) (downloads.Result, error) {
	var douyinFallbackErr error
	if ref, parseErr := discovery.ParseDouyin(rawURL); parseErr == nil && ref.ID != "" {
		if result, fallbackErr := t.downloadDouyinFallback(ctx, ref.URL, directory, height, options); fallbackErr == nil {
			return result, nil
		} else {
			douyinFallbackErr = fallbackErr
		}
	}
	if !t.Ready() {
		if douyinFallbackErr != nil {
			if options.Mode == "audio" {
				return downloads.Result{}, douyinFallbackErr
			}
			return downloads.Result{}, fmt.Errorf("应用内抖音页面下载失败，且 yt-dlp 尚未就绪：%v", douyinFallbackErr)
		}
		return downloads.Result{}, downloads.ErrTools
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return downloads.Result{}, errors.New("无法创建下载目录，请检查磁盘权限。")
	}
	cookies, clean, err := t.cookieArguments(rawURL)
	if err != nil {
		return downloads.Result{}, downloads.ErrTools
	}
	defer clean()
	args := append(cookies, t.mediaArguments(rawURL, directory, height, options)...)
	cmd, err := t.ytdlpCommand(ctx, args...)
	if err != nil {
		if err = t.toolDownloadError(ctx, err); errors.Is(err, downloads.ErrTools) {
			err = errors.New("yt-dlp 未能下载，请检查网络后重试；工具设置中可查看下载状态。")
		}
		return downloads.Result{}, err
	}
	stdout, stderr := &boundedBuffer{}, &boundedBuffer{}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return downloads.Result{}, ctx.Err()
		}
		if ref, parseErr := discovery.ParseDouyin(rawURL); parseErr == nil && ref.ID != "" {
			cookieArgs, _, _ := t.cookieArguments(rawURL)
			return downloads.Result{}, classifyDouyin(stderr.String(), len(cookieArgs) > 0)
		}
		return downloads.Result{}, classify(stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	abs, err := filepath.Abs(strings.TrimSpace(lines[len(lines)-1]))
	root, rootErr := filepath.Abs(directory)
	if err != nil || rootErr != nil || filepath.Dir(abs) != root {
		return downloads.Result{}, errors.New("下载工具返回了无效的文件位置。")
	}
	info, err := os.Lstat(abs)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return downloads.Result{}, errors.New("下载未产生完整的媒体文件。")
	}
	result := downloads.Result{FileName: filepath.Base(abs), FileSize: info.Size(), OriginalSize: info.Size()}
	if options.Mode != "audio" && (options.VideoOnly || options.Compress) {
		return t.optimizeVideo(ctx, abs, options, result)
	}
	return result, nil
}

func (t *Toolkit) optimizeVideo(ctx context.Context, file string, options downloads.Options, result downloads.Result) (downloads.Result, error) {
	compress := options.Compress
	if compress {
		// Avoid silently reducing HDR or high-bit-depth sources to 8-bit SDR.
		ffprobe := executable(filepath.Join(filepath.Dir(t.config.FFmpegPath), "ffprobe"), "")
		if ffprobe == "" {
			ffprobe = executable("", "ffprobe")
		}
		cmd := command(ctx, ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=color_transfer,pix_fmt", "-of", "json", file)
		cmd.Env = toolEnvironment()
		output, stderr := &metadataBuffer{limit: 16384}, &boundedBuffer{}
		cmd.Stdout, cmd.Stderr = output, stderr
		var probe struct {
			Streams []struct {
				Transfer    string `json:"color_transfer"`
				PixelFormat string `json:"pix_fmt"`
			} `json:"streams"`
		}
		if cmd.Run() != nil || output.exceeded || json.Unmarshal(output.Bytes(), &probe) != nil || len(probe.Streams) == 0 {
			compress = false
			result.Message = "无法确认视频色彩信息，已保留原始画质。"
		} else {
			stream := probe.Streams[0]
			if stream.Transfer == "smpte2084" || stream.Transfer == "arib-std-b67" || strings.Contains(stream.PixelFormat, "10") || strings.Contains(stream.PixelFormat, "12") || strings.Contains(stream.PixelFormat, "16") {
				compress = false
				result.Message = "高动态范围或高位深视频已保留原始画质。"
			}
		}
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if !compress && !options.VideoOnly {
		return result, nil
	}
	temp := filepath.Join(filepath.Dir(file), ".optimized"+filepath.Ext(file))
	defer os.Remove(temp)
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-i", file, "-map", "0:v:0", "-map_metadata", "0", "-map_chapters", "0"}
	if !options.VideoOnly {
		args = append(args, "-map", "0:a?", "-c:a", "copy")
	}
	if compress {
		args = append(args, "-c:v", "libx264", "-crf", "22", "-preset", "medium", "-threads", "2", "-pix_fmt", "yuv420p", "-fps_mode", "passthrough", "-vf", "pad=ceil(iw/2)*2:ceil(ih/2)*2")
	} else {
		args = append(args, "-c:v", "copy")
	}
	if strings.EqualFold(filepath.Ext(file), ".mp4") {
		args = append(args, "-movflags", "+faststart")
	}
	args = append(args, temp)
	cmd := command(ctx, t.config.FFmpegPath, args...)
	cmd.Env = toolEnvironment()
	output := &boundedBuffer{}
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, errors.New("视频压缩或音轨处理失败，请关闭节省空间后重试，或检查 FFmpeg 配置。")
	}
	info, err := os.Lstat(temp)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return result, errors.New("视频处理没有生成完整文件。")
	}
	if info.Size() >= result.OriginalSize && !options.VideoOnly {
		result.Message = "原文件已较紧凑，保留体积更小的原始文件。"
		return result, nil
	}
	if info.Size() >= result.OriginalSize && compress {
		// Audio removal must still happen even when transcoding adds bytes.
		options.Compress = false
		return t.optimizeVideo(ctx, file, options, result)
	}
	if err := os.Rename(temp, file); err != nil {
		return result, errors.New("无法保存处理后的视频，请检查磁盘权限。")
	}
	result.FileSize, result.Compressed = info.Size(), compress
	return result, nil
}

// toolDownloadError explains a failed or unfinished first-use yt-dlp download.
func (t *Toolkit) toolDownloadError(ctx context.Context, err error) error {
	if t.ytdlp != nil && t.ytdlp.snapshot().Downloading {
		return downloads.ErrToolBusy
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return downloads.ErrTools
}
