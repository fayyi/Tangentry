package tools

import (
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"

	"tangentry/internal/subtitles"
	"tangentry/internal/transcription"
)

const maxStreamingPCM = 16000 * 2 * 15

// RecognizePCM provides a local-only streaming fallback for whisper.cpp. It
// deliberately invokes the already configured local model for bounded chunks;
// the session merger handles partial/final cue continuity while a future native
// worker can replace this adapter without changing the API.
func (t *Transcriber) RecognizePCM(ctx context.Context, cfg transcription.Config, pcm []byte, offset float64) (subtitles.Track, error) {
	if cfg.Cloud() || cfg.Backend != "whisper.cpp" || len(pcm) == 0 || len(pcm) > maxStreamingPCM || len(pcm)%2 != 0 || offset < 0 {
		return subtitles.Track{}, transcription.ErrInvalid
	}
	// Prefer the resident HTTP worker so the model is loaded once for the
	// session. Environments that only ship whisper-cli retain the existing
	// bounded-process fallback.
	if server, err := t.residentServer(ctx, cfg); err == nil {
		track, err := server.recognize(ctx, pcm, cfg.Language)
		if err != nil {
			return subtitles.Track{}, err
		}
		for index := range track.Cues {
			track.Cues[index].Start += offset
			track.Cues[index].End += offset
		}
		return track, nil
	}
	if !t.Status(cfg).Ready {
		return subtitles.Track{}, transcription.ErrNotReady
	}
	root, err := os.MkdirTemp("", "tangentry-streaming-*")
	if err != nil {
		return subtitles.Track{}, transcription.ErrUnavailable
	}
	defer os.RemoveAll(root)
	wav := filepath.Join(root, "chunk.wav")
	if err := writePCM16WAV(wav, pcm, 16000, 1); err != nil {
		return subtitles.Track{}, transcription.ErrUnavailable
	}
	output := filepath.Join(root, "transcript")
	cmd := command(ctx, executable(cfg.Binary, "whisper-cli"), "-m", cfg.Model, "-f", wav, "-l", cfg.Language, "-oj", "-of", output, "-t", "4", "-nt")
	cmd.Env = toolEnvironment()
	cmd.Stdout = io.Discard
	warnings := &boundedBuffer{}
	cmd.Stderr = warnings
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return subtitles.Track{}, ctx.Err()
		}
		return subtitles.Track{}, toolFailure("streaming_recognizing", warnings.String())
	}
	file, err := os.Open(output + ".json")
	if err != nil {
		return subtitles.Track{}, transcription.Failed("missing_transcript", "识别工具未生成实时字幕片段。")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return subtitles.Track{}, subtitles.ErrFormat
	}
	track, err := parseWhisper(data, 0, cfg.Language)
	if err != nil {
		return subtitles.Track{}, err
	}
	for index := range track.Cues {
		track.Cues[index].Start += offset
		track.Cues[index].End += offset
	}
	track.Source = "automatic"
	track.Label = "实时本机字幕"
	return track, nil
}

// StreamingReady is a lightweight capability check used before a playback
// session starts. It avoids accepting PCM frames for local engines that only
// implement the batch transcription contract.
func (t *Transcriber) StreamingReady() bool {
	return executable(t.StreamingBinary, "whisper-server") != "" || executable("", "whisper-cli") != ""
}

func writePCM16WAV(path string, pcm []byte, sampleRate, channels int) error {
	if len(pcm) == 0 || sampleRate <= 0 || channels <= 0 {
		return subtitles.ErrFormat
	}
	dataSize := uint32(len(pcm))
	byteRate := uint32(sampleRate * channels * 2)
	blockAlign := uint16(channels * 2)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	write := func(value any) error { return binary.Write(file, binary.LittleEndian, value) }
	if _, err := io.WriteString(file, "RIFF"); err != nil {
		return err
	}
	if err := write(uint32(36) + dataSize); err != nil {
		return err
	}
	if _, err := io.WriteString(file, "WAVEfmt "); err != nil {
		return err
	}
	if err := write(uint32(16)); err != nil {
		return err
	}
	if err := write(uint16(1)); err != nil {
		return err
	}
	if err := write(uint16(channels)); err != nil {
		return err
	}
	if err := write(uint32(sampleRate)); err != nil {
		return err
	}
	if err := write(byteRate); err != nil {
		return err
	}
	if err := write(blockAlign); err != nil {
		return err
	}
	if err := write(uint16(16)); err != nil {
		return err
	}
	if _, err := io.WriteString(file, "data"); err != nil {
		return err
	}
	if err := write(dataSize); err != nil {
		return err
	}
	_, err = file.Write(pcm)
	return err
}
