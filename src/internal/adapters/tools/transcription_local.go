package tools

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"tangentry/internal/subtitles"
	"tangentry/internal/transcription"
)

// transcribeLocal contains only the FFmpeg and local engine pipeline.
func (t *Transcriber) transcribeLocal(ctx context.Context, cfg transcription.Config, input, root string, limit int, stage func(string)) (subtitles.Track, error) {
	wav := filepath.Join(root, "audio.wav")
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-protocol_whitelist", "file,pipe", "-format_whitelist", "mp3,mov,aac,ogg,flac,wav,matroska,webm", "-i", input}
	if limit > 0 {
		args = append(args, "-t", "60")
	}
	args = append(args, "-vn", "-ar", "16000", "-ac", "1", "-c:a", "pcm_s16le", "-y", wav)
	cmd := command(ctx, executable(t.FFmpeg, "ffmpeg"), args...)
	cmd.Env = toolEnvironment()
	cmd.Stdout = io.Discard
	warnings := &boundedBuffer{}
	cmd.Stderr = warnings
	if cmd.Run() != nil {
		return subtitles.Track{}, toolFailure("converting", warnings.String())
	}
	stage("recognizing")
	output := filepath.Join(root, "transcript")
	if cfg.Backend == "faster-whisper" || cfg.Backend == "qwen3-asr" {
		helper := filepath.Join(root, "recognize.py")
		script := fasterScript
		if cfg.Backend == "qwen3-asr" {
			script = qwenScript
		}
		if err := os.WriteFile(helper, script, 0600); err != nil {
			return subtitles.Track{}, transcription.Failed("temporary_storage", "无法写入转写临时文件，请检查临时目录权限与磁盘空间。")
		}
		cmd = command(ctx, executable(cfg.Python, "python3"), helper, cfg.Model, wav, cfg.Language, output+".json")
		if cfg.Backend == "qwen3-asr" {
			cmd.Args = append(cmd.Args, cfg.QwenAligner)
		}
	} else {
		cmd = command(ctx, executable(cfg.Binary, "whisper-cli"), "-m", cfg.Model, "-f", wav, "-l", cfg.Language, "-oj", "-of", output, "-t", "4", "-nt")
	}
	cmd.Env = toolEnvironment()
	cmd.Stdout = io.Discard
	warnings = &boundedBuffer{}
	cmd.Stderr = warnings
	if cmd.Run() != nil {
		return subtitles.Track{}, toolFailure("recognizing", warnings.String())
	}
	jsonFile, err := os.Open(output + ".json")
	if err != nil {
		return subtitles.Track{}, transcription.Failed("missing_transcript", "识别工具未生成字幕文件，请检查模型与识别工具配置。")
	}
	defer jsonFile.Close()
	data, err := io.ReadAll(io.LimitReader(jsonFile, (4<<20)+1))
	if err != nil || len(data) > 4<<20 {
		return subtitles.Track{}, subtitles.ErrFormat
	}
	return parseWhisper(data, limit, cfg.Language)
}
