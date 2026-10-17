package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestModelDirectoryMovesFilesAndSurvivesRestart(t *testing.T) {
	t.Setenv("TANGENTRY_WHISPER_MODELS_DIR", "")
	t.Setenv("TANGENTRY_WHISPER_MODEL", "")
	data := t.TempDir()
	assets := fstest.MapFS{"index.html": {Data: []byte("fixture")}}
	application, err := New(data, assets)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { application.Close() }()
	stable := filepath.Join(data, "models")
	if err := os.MkdirAll(stable, 0700); err != nil {
		t.Fatal(err)
	}
	model := filepath.Join(stable, "ggml-custom.bin")
	if err := os.WriteFile(model, []byte("model fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	c := application.runtime.transcription.Configuration()
	c.Model = model
	if _, err := application.runtime.transcription.Configure(c); err != nil {
		t.Fatal(err)
	}
	first := t.TempDir()
	if err := application.SetModelDirectory(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	verify := func() {
		t.Helper()
		body, err := os.ReadFile(model)
		if err != nil || string(body) != "model fixture" {
			t.Fatal("stable model unavailable", err)
		}
		if application.runtime.transcription.Configuration().Model != model {
			t.Fatal("stored path changed")
		}
	}
	verify()
	second := t.TempDir()
	if err := application.SetModelDirectory(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	verify()
	if _, err := os.Stat(filepath.Join(first, "Tangentry Models")); !os.IsNotExist(err) {
		t.Fatal("previous models were not moved", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := application.SetModelDirectory(ctx, t.TempDir()); err == nil {
		t.Fatal("canceled migration succeeded")
	}
	verify()
	if err := application.Close(); err != nil {
		t.Fatal(err)
	}
	application, err = New(data, assets)
	if err != nil {
		t.Fatal(err)
	}
	verify()
	resolved, _ := filepath.EvalSymlinks(filepath.Join(second, "Tangentry Models"))
	if application.ModelDirectory() != resolved {
		t.Fatal("destination lost on restart")
	}
}
