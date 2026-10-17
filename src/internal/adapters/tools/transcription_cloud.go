package tools

import (
	"context"
	"io"
	"math"
	"path/filepath"
	"strconv"
	"time"

	"tangentry/internal/subtitles"
	"tangentry/internal/transcription"
)

func (t *Transcriber) transcribeCloud(ctx context.Context, cfg transcription.Config, input, root string, limit int, stage func(string)) (subtitles.Track, error) {
	seconds := "600"
	if cfg.Backend == "cloudflare" {
		seconds = "300"
	}
	if cfg.Backend == "siliconflow" {
		seconds = strconv.Itoa(cfg.SiliconflowSegmentSeconds)
		if cfg.SiliconflowSegmentSeconds == 0 {
			seconds = "30"
		}
	}
	manifest := filepath.Join(root, "chunks.csv")
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-protocol_whitelist", "file,pipe",
		"-format_whitelist", "mp3,mov,aac,ogg,flac,wav,matroska,webm", "-i", input}
	if limit > 0 {
		args = append(args, "-t", strconv.Itoa(limit))
	} else {
		args = append(args, "-t", "21601")
	}
	// The segment muxer writes independently decodable MP3s and their actual time offsets.
	args = append(args, "-vn", "-ar", "16000", "-ac", "1", "-c:a", "libmp3lame", "-b:a", "32k",
		"-f", "segment", "-segment_time", seconds, "-segment_list", manifest, "-segment_list_type", "csv",
		"-reset_timestamps", "1", "-y", filepath.Join(root, "chunk-%03d.mp3"))
	cmd := command(ctx, executable(t.FFmpeg, "ffmpeg"), args...)
	cmd.Env = toolEnvironment()
	warnings := &boundedBuffer{}
	cmd.Stdout, cmd.Stderr = io.Discard, warnings
	if cmd.Run() != nil {
		return subtitles.Track{}, toolFailure("converting", warnings.String())
	}
	chunks, err := readSpeechChunks(manifest, root)
	if err != nil {
		return subtitles.Track{}, err
	}
	cache, err := newSpeechCache(t.CacheDir)
	if err != nil {
		return subtitles.Track{}, err
	}
	result := subtitles.Track{Language: "whisper", Label: cloudTrackLabel(cfg, seconds, limit), Automatic: true, Cues: []subtitles.Cue{}}
	uploaded := false
	for _, chunk := range chunks {
		if limit > 0 && chunk.start >= float64(limit) {
			break
		}
		if err := ctx.Err(); err != nil {
			return subtitles.Track{}, err
		}
		duration := chunk.end - chunk.start
		if limit > 0 {
			duration = math.Min(duration, float64(limit)-chunk.start)
		}
		key := ""
		if cache != nil {
			key, err = speechCacheKey(ctx, cfg, chunk, duration)
			if err != nil {
				return subtitles.Track{}, err
			}
		}
		cues, reused, cacheErr := cache.load(key, duration)
		if cacheErr != nil {
			return subtitles.Track{}, cacheErr
		}
		if reused {
			stage("cached")
		}
		if !reused && cfg.Backend == "siliconflow" && uploaded {
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return subtitles.Track{}, ctx.Err()
			case <-timer.C:
			}
		}
		if !reused {
			stage("uploading")
			data, requestErr := t.cloudRequest(ctx, cfg, chunk.path)
			if err := ctx.Err(); err != nil {
				return subtitles.Track{}, err
			}
			if requestErr != nil {
				return subtitles.Track{}, requestErr
			}
			uploaded = true
			if cfg.Backend == "siliconflow" {
				cues, err = parseSiliconflowSpeech(data, cfg.Language, duration)
			} else {
				cues, err = parseCloudSpeech(data, cfg.Language, duration)
			}
			if err != nil {
				return subtitles.Track{}, err
			}
			if err := ctx.Err(); err != nil {
				return subtitles.Track{}, err
			}
			if err := cache.save(key, cues, duration); err != nil {
				return subtitles.Track{}, err
			}
		}
		for _, cue := range cues {
			cue.Start += chunk.start
			cue.End += chunk.start
			result.Cues = append(result.Cues, cue)
		}
		if len(result.Cues) > 10000 {
			return subtitles.Track{}, subtitles.ErrFormat
		}
	}
	if err := ctx.Err(); err != nil {
		return subtitles.Track{}, err
	}
	if len(result.Cues) == 0 {
		return subtitles.Track{}, transcription.Failed("no_speech", "音频中未识别到带时间戳的语音。")
	}
	return result, nil
}

func cloudTrackLabel(cfg transcription.Config, seconds string, limit int) string {
	label := "Groq · 语音转写"
	switch cfg.Backend {
	case "cloudflare":
		label = "Cloudflare · 语音转写"
	case "siliconflow":
		label = "硅基流动 · 片段字幕（约 " + seconds + " 秒/段）"
		if cfg.SiliconflowModel == "Qwen/Qwen3-ASR-1.7B" {
			label = "硅基流动 · Qwen3-ASR · 片段字幕（约 " + seconds + " 秒/段）"
		}
	case "mai":
		label = "Microsoft Playground · MAI-Transcribe-2"
	}
	if limit > 0 {
		label = "前 60 秒 · " + label
	}
	return label
}
