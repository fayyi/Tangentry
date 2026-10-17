package tools

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"tangentry/internal/discovery"
	"tangentry/internal/transcription"
)

// Resolve one combined HTTPS audio file; the speech engine performs the bounded
// fetch. No Cookie header or signed CDN URL crosses the HTTP API boundary.
func (t *Toolkit) YouTubeAudio(ctx context.Context, id string) (transcription.Audio, error) {
	parsed, canonical, err := discovery.ParseYouTube("https://www.youtube.com/watch?v=" + id)
	if err != nil || parsed != id {
		return transcription.Audio{}, transcription.ErrInvalid
	}
	return t.resolveToolAudio(ctx, "youtube", id, canonical)
}

func (t *Toolkit) DouyinAudio(ctx context.Context, id string) (transcription.Audio, error) {
	ref, err := discovery.ParseDouyin("https://www.douyin.com/video/" + id)
	if err != nil || ref.ID != id {
		return transcription.Audio{}, transcription.ErrInvalid
	}
	return t.resolveToolAudio(ctx, "douyin", id, ref.URL)
}

func (t *Toolkit) XiaohongshuAudio(ctx context.Context, id, raw string) (transcription.Audio, error) {
	ref, err := discovery.ParseXiaohongshu(raw)
	if err != nil || ref.ID != id {
		return transcription.Audio{}, transcription.ErrInvalid
	}
	return t.resolveToolAudio(ctx, "xiaohongshu", id, ref.URL)
}

func (t *Toolkit) resolveToolAudio(ctx context.Context, platform, id, canonical string) (transcription.Audio, error) {
	if !t.ytdlpReady() {
		return transcription.Audio{}, transcription.ErrNotReady
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cookies, clean, err := t.cookieArguments(canonical)
	if err != nil {
		return transcription.Audio{}, transcription.ErrUnavailable
	}
	defer clean()
	args := []string{"--ignore-config", "--no-playlist", "--no-cache-dir", "--no-warnings", "--skip-download", "--dump-single-json", "--format", "ba[protocol=https]/b[protocol=https]", "--socket-timeout", "10", "--retries", "0"}
	args = append(args, cookies...)
	args = append(args, "--", canonical)
	cmd, err := t.ytdlpCommand(ctx, args...)
	if err != nil {
		return transcription.Audio{}, transcription.ErrNotReady
	}
	output, warnings := &metadataBuffer{}, &boundedBuffer{}
	cmd.Stdout, cmd.Stderr = output, warnings
	if cmd.Run() != nil || output.exceeded {
		if ctx.Err() != nil {
			return transcription.Audio{}, ctx.Err()
		}
		return transcription.Audio{}, transcription.ErrUnavailable
	}
	var data struct {
		ID       string  `json:"id"`
		URL      string  `json:"url"`
		Protocol string  `json:"protocol"`
		Duration float64 `json:"duration"`
	}
	if json.Unmarshal(output.Bytes(), &data) != nil || data.ID != id || data.Protocol != "https" || math.IsNaN(data.Duration) || math.IsInf(data.Duration, 0) || data.Duration < 0 || data.Duration > 21600 {
		return transcription.Audio{}, transcription.ErrInvalid
	}
	if !speechMediaURL(platform, data.URL) {
		return transcription.Audio{}, transcription.ErrInvalid
	}
	return transcription.Audio{URL: data.URL, Duration: int(data.Duration), Platform: platform}, nil
}
