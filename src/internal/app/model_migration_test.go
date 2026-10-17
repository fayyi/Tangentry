package app

import (
	"context"
	"path/filepath"
	"tangentry/internal/adapters/filesystem"
	"testing"

	"tangentry/internal/subtitles"
	"tangentry/internal/transcription"
)

type idleEngine struct{}

func (idleEngine) Status(c transcription.Config) transcription.Status {
	return transcription.Status{Config: c, Ready: true}
}
func (idleEngine) Transcribe(context.Context, transcription.Config, transcription.Audio, int, func(string)) (subtitles.Track, error) {
	return subtitles.Track{}, transcription.ErrUnavailable
}

func TestSettingsNamingTheFormerlyBundledModelMoveToTheManagedDirectory(t *testing.T) {
	root := t.TempDir()
	modelDir := filepath.Join(root, "models")
	bundled := filepath.Join(root, "runtime", "models", "ggml-base.bin")
	settings := filepath.Join(root, "transcription.json")
	speech, err := transcription.NewWithStore(filesystem.NewTranscriptionSettings(settings), transcription.Config{Backend: "whisper.cpp", Binary: "whisper-cli", Model: bundled, Language: "zh"}, idleEngine{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := speech.Configure(speech.Configuration()); err != nil {
		t.Fatal(err)
	}
	if err := migrateBundledModel(speech, modelDir); err != nil {
		t.Fatal(err)
	}
	if got := speech.Configuration().Model; got != filepath.Join(modelDir, "ggml-base.bin") {
		t.Fatal("bundled model path survived", got)
	}
	reloaded, err := transcription.NewWithStore(filesystem.NewTranscriptionSettings(settings), transcription.Config{}, idleEngine{}, nil, nil)
	if err != nil || reloaded.Configuration().Model != filepath.Join(modelDir, "ggml-base.bin") {
		t.Fatal("migration was not persisted", err)
	}
	custom := filepath.Join(root, "elsewhere", "ggml-small.bin")
	other, _ := transcription.NewWithStore(filesystem.NewTranscriptionSettings(filepath.Join(root, "other.json")), transcription.Config{Backend: "whisper.cpp", Binary: "whisper-cli", Model: custom, Language: "zh"}, idleEngine{}, nil, nil)
	if err := migrateBundledModel(other, modelDir); err != nil || other.Configuration().Model != custom {
		t.Fatal("a user-selected model path was rewritten")
	}
}
