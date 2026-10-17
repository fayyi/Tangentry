package transcription_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"tangentry/internal/adapters/filesystem"
	"testing"

	"tangentry/internal/subtitles"
	"tangentry/internal/transcription"
)

func TestCloudSecretsPersistPrivatelyAndCanBeCleared(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	service, err := transcription.NewWithStore(filesystem.NewTranscriptionSettings(path), transcription.Config{Backend: "whisper.cpp", Binary: "fixture", Model: "fixture.bin", Language: "zh"}, testEngine{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	key, token, sfKey := "groq-secret-fixture", "cf-secret-fixture", "sf-secret-fixture"
	status, err := service.ConfigureSettings(transcription.SettingsInput{Config: transcription.Config{Backend: "groq", Language: "zh"}, GroqAPIKey: &key, CloudflareAPIToken: &token, SiliconflowAPIKey: &sfKey})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(status)
	if strings.Contains(string(raw), key) || strings.Contains(string(raw), token) || strings.Contains(string(raw), sfKey) || !status.GroqKeyConfigured || !status.CloudflareTokenConfigured || !status.SiliconflowKeyConfigured || status.Config.GroqAPIKey != "" {
		t.Fatal("secret exposed or missing presence state")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("insecure settings file")
	}
	reopened, err := transcription.NewWithStore(filesystem.NewTranscriptionSettings(path), transcription.Config{}, testEngine{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.Configuration().GroqAPIKey != key || reopened.Configuration().SiliconflowAPIKey != sfKey {
		t.Fatal("secret not persisted")
	}
	cfg := status.Config
	cfg.Backend = "siliconflow"
	if _, err := reopened.Configure(cfg); err != nil {
		t.Fatal(err)
	}
	if reopened.Configuration().GroqAPIKey != key || reopened.Configuration().CloudflareAPIToken != token {
		t.Fatal("engine switch discarded saved keys")
	}
	empty := ""
	status, err = reopened.ConfigureSettings(transcription.SettingsInput{Config: cfg, GroqAPIKey: &empty, SiliconflowAPIKey: &empty})
	if err != nil || status.GroqKeyConfigured || status.SiliconflowKeyConfigured || !status.CloudflareTokenConfigured {
		t.Fatal("targeted clear failed", err)
	}
}

func TestMAISessionAndQwenSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := transcription.NewWithStore(filesystem.NewTranscriptionSettings(path), transcription.Config{}, testEngine{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cookie := "session=private-fixture; other=fixture"
	cfg := transcription.Config{Backend: "mai", Language: "zh", SiliconflowModel: "Qwen/Qwen3-ASR-1.7B"}
	status, err := s.ConfigureSettings(transcription.SettingsInput{Config: cfg, MAICookie: &cookie})
	raw, _ := json.Marshal(status)
	if err != nil || !status.MAICookieConfigured || strings.Contains(string(raw), "private-fixture") {
		t.Fatal("session exposed", err)
	}
	reopened, err := transcription.NewWithStore(filesystem.NewTranscriptionSettings(path), transcription.Config{}, testEngine{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.Configuration().MAICookie != cookie || reopened.Configuration().SiliconflowModel != cfg.SiliconflowModel {
		t.Fatal("settings lost")
	}
	bad := "session=foo\r\nInjected: true"
	if _, err := s.ConfigureSettings(transcription.SettingsInput{Config: cfg, MAICookie: &bad}); err == nil {
		t.Fatal("header injection accepted")
	}
	cfg.SiliconflowModel = "unknown"
	if _, err := s.Configure(cfg); err == nil {
		t.Fatal("unknown model accepted")
	}
	cfg.SiliconflowModel = ""
	empty := ""
	status, err = s.ConfigureSettings(transcription.SettingsInput{Config: cfg, MAICookie: &empty})
	if err != nil || status.MAICookieConfigured {
		t.Fatal("session not cleared")
	}
}

func TestCloudRequiresConsentForTheActualProviderAndKeepsCoarseTracksSeparate(t *testing.T) {
	service, saved := setup(t, testEngine{})
	id := "rss:" + strings.Repeat("a", 64)
	cfg := transcription.Config{Backend: "siliconflow", Language: "zh", SiliconflowSegmentSeconds: 30}
	if _, err := service.Configure(cfg); err != nil {
		t.Fatal(err)
	}
	for _, option := range []struct {
		consent bool
		backend string
	}{{false, "siliconflow"}, {true, "groq"}, {true, ""}} {
		if _, err := service.StartWithUploadConsent(context.Background(), id, true, option.consent, option.backend); !errors.Is(err, transcription.ErrInvalid) {
			t.Fatal("unconsented provider upload accepted")
		}
	}
	precise := "original precise subtitle"
	_, err := saved.Save(context.Background(), id, subtitles.Track{Language: "whisper", Label: "precise fixture", Cues: []subtitles.Cue{{Start: 1, End: 3, Text: precise}}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	job, err := service.StartWithUploadConsent(context.Background(), id, true, true, "siliconflow")
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, service, job.ID, "completed")
	tracks, err := saved.List(context.Background(), id)
	if err != nil || len(tracks) != 2 {
		t.Fatal("coarse track overwrote accurate one")
	}
	for _, value := range tracks {
		if value.Track.Language == "whisper" && value.Track.Cues[0].Text != precise {
			t.Fatal("accurate subtitle changed")
		}
		if value.Track.Language != "whisper" && value.Track.Language != "siliconflow-preview" {
			t.Fatal("wrong separate track id")
		}
	}
}
