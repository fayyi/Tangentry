package tools

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"tangentry/internal/subtitles"
	"tangentry/internal/transcription"
)

// Transcriber is the adapter boundary for local and cloud speech engines.
// Command-line, Python and HTTP details are implemented in adjacent files.
type Transcriber struct {
	ModelDir  string
	FFmpeg    string
	YTDLP     string
	HTTP      *http.Client
	CloudHTTP *http.Client // Separate client; source-media requests never receive cloud credentials.
	CacheDir  string
	// Models downloads a missing catalog whisper.cpp model on first use.
	Models *ModelInstaller
	// ResolveYTDLP, when set, supplies the managed yt-dlp instead of YTDLP.
	ResolveYTDLP func(context.Context) (string, error)
}

// Transcribe owns the request lifecycle and stage ordering. Engine-specific
// work is delegated after the input has been made available locally.
func (t *Transcriber) Transcribe(ctx context.Context, cfg transcription.Config, audio transcription.Audio, limit int, stage func(string)) (subtitles.Track, error) {
	if !t.Status(cfg).Ready {
		return subtitles.Track{}, transcription.ErrNotReady
	}
	if audio.Duration > 6*3600 {
		return subtitles.Track{}, transcription.ErrInvalid
	}
	if option, ok := t.pendingModel(cfg); ok {
		stage("model")
		if err := t.Models.Install(ctx, option, "", func(string) {}); err != nil {
			if ctx.Err() != nil {
				return subtitles.Track{}, ctx.Err()
			}
			return subtitles.Track{}, transcription.Failed("model_download", "Whisper 模型未能完整下载，请检查网络与磁盘空间后重试；已下载的部分不会被使用。")
		}
	}
	root, err := os.MkdirTemp("", "tangentry-transcription-*")
	if err != nil {
		return subtitles.Track{}, storageFailure(err)
	}
	defer os.RemoveAll(root)
	input, err := t.prepareInput(ctx, audio, root, stage)
	if err != nil {
		return subtitles.Track{}, err
	}
	stage("converting")
	if cfg.Cloud() {
		return t.transcribeCloud(ctx, cfg, input, root, limit, stage)
	}
	return t.transcribeLocal(ctx, cfg, input, root, limit, stage)
}

func (t *Transcriber) prepareInput(ctx context.Context, audio transcription.Audio, root string, stage func(string)) (string, error) {
	input := filepath.Join(root, "source.media")
	if audio.LocalPath != "" {
		stage("local")
		info, err := os.Lstat(audio.LocalPath)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return "", transcription.Failed("local_media", "已下载的媒体文件已移走、为空或不可读取，请重新下载后重试。")
		}
		return audio.LocalPath, nil
	}
	stage("fetching")
	fetchCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	if err := t.fetchSpeechAudio(fetchCtx, audio, input); err != nil {
		return "", err
	}
	return input, nil
}
