package transcription_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"tangentry/internal/adapters/filesystem"
	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/adapters/tools"
	"tangentry/internal/subtitles"
	"tangentry/internal/transcription"
	"testing"
	"time"
)

type testEngine struct {
	gate    chan struct{}
	failure error
	configs chan transcription.Config
	// download reports a first-use model download before recognition.
	download bool
}

func TestModelChangesApplyToNextJobWithoutChangingRunningJob(t *testing.T) {
	gate := make(chan struct{})
	configs := make(chan transcription.Config, 2)
	s, _ := setup(t, testEngine{gate: gate, configs: configs})
	first := transcription.Config{Backend: "faster-whisper", Python: "/fixture/python", Model: "/fixture/base", Language: "zh"}
	if _, err := s.Configure(first); err != nil {
		t.Fatal(err)
	}
	job, err := s.Start(context.Background(), "bilibili:BV1QraJ6ZESK", true)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case cfg := <-configs:
		if cfg != first {
			t.Fatal("first task ignored explicit configuration", cfg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("first task did not reach the engine")
	}
	second := first
	second.Model, second.Language = "/fixture/turbo", "auto"
	if _, err := s.Configure(second); err != nil {
		t.Fatal(err)
	}
	close(gate)
	waitJob(t, s, job.ID, "completed")
	// Completion is published before the worker releases the active-task gate.
	deadline := time.Now().Add(3 * time.Second)
	for {
		job, err = s.Start(context.Background(), "bilibili:BV17x411w7KC", true)
		if !errors.Is(err, transcription.ErrBusy) || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, s, job.ID, "completed")
	select {
	case cfg := <-configs:
		if cfg != second {
			t.Fatal("next task retained the previous model", cfg)
		}
	default:
		t.Fatal("next task did not capture configuration")
	}
}

func TestFasterWhisperEnvironmentCanBeSavedBeforeDownloadingModel(t *testing.T) {
	root := t.TempDir()
	config := transcription.Config{Backend: "faster-whisper", Python: filepath.Join(root, "python"), Language: "auto"}
	engine := &tools.Transcriber{FFmpeg: filepath.Join(root, "ffmpeg"), ModelDir: filepath.Join(root, "models")}
	resolves := 0
	resolve := func(context.Context, string) (transcription.Audio, error) {
		resolves++
		return transcription.Audio{}, nil
	}
	path := filepath.Join(root, "settings.json")
	service, err := transcription.NewWithStore(filesystem.NewTranscriptionSettings(path), config, engine, resolve, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	status, err := service.Configure(config)
	if err != nil || status.Ready || len(status.Missing) == 0 {
		t.Fatalf("environment save must succeed without declaring readiness: %+v, %v", status, err)
	}
	reloaded, err := transcription.NewWithStore(filesystem.NewTranscriptionSettings(path), config, engine, resolve, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	if reloaded.Configuration().Python != config.Python || reloaded.Configuration().Model != "" {
		t.Fatal("environment was not persisted independently of the model")
	}
	if _, err := reloaded.Start(context.Background(), "rss:"+strings.Repeat("a", 64), false); !errors.Is(err, transcription.ErrNotReady) {
		t.Fatalf("missing model should prevent transcription: %v", err)
	}
	if resolves != 0 {
		t.Fatal("incomplete environment triggered an audio request")
	}
	config.Python = ""
	if _, err := service.Configure(config); !errors.Is(err, transcription.ErrInvalid) {
		t.Fatalf("missing Python should remain invalid: %v", err)
	}
}

func TestLocalQwenConfigurationPersistsWithoutCloudConsent(t *testing.T) {
	s, _ := setup(t, testEngine{})
	cfg := transcription.Config{Backend: "qwen3-asr", Python: "/local/python", Model: "/local/asr", QwenAligner: "/local/aligner", Language: "zh"}
	if cfg.Cloud() {
		t.Fatal("local Qwen treated as cloud")
	}
	if _, err := s.Configure(cfg); err != nil {
		t.Fatal(err)
	}
	if s.Configuration().QwenAligner != cfg.QwenAligner {
		t.Fatal("aligner not persisted")
	}
	job, err := s.Start(context.Background(), "xiaoyuzhou:"+strings.Repeat("a", 24), true)
	if err != nil {
		t.Fatal("local job required cloud consent", err)
	}
	waitJob(t, s, job.ID, "completed")
	for _, bad := range []transcription.Config{
		{Backend: "qwen3-asr", Model: cfg.Model, Language: "zh"},
	} {
		if _, err := s.Configure(bad); err == nil {
			t.Fatal("incomplete local Qwen accepted")
		}
	}
}

func (e testEngine) Status(c transcription.Config) transcription.Status {
	return transcription.Status{Config: c, Ready: true, Missing: []string{}}
}
func (e testEngine) Transcribe(ctx context.Context, c transcription.Config, a transcription.Audio, limit int, stage func(string)) (subtitles.Track, error) {
	if e.download {
		stage("model")
		transcription.ReportProgress(ctx, 50, 100)
		<-e.gate
	}
	stage("recognizing")
	if e.configs != nil {
		e.configs <- c
	}
	if e.failure != nil {
		return subtitles.Track{}, e.failure
	}
	if e.gate != nil {
		select {
		case <-e.gate:
		case <-ctx.Done():
			return subtitles.Track{}, ctx.Err()
		}
	}
	return subtitles.Track{Label: "明确标识的识别夹具", Automatic: true, Cues: []subtitles.Cue{{Start: 1, End: 4, Text: "转写测试文字"}}}, nil
}

func TestSafeFailureRetainsStageAndDoesNotSavePartialCaptions(t *testing.T) {
	s, saved := setup(t, testEngine{failure: transcription.Failed("model_memory", "识别模型所需内存不足，请选择较小模型后重试。")})
	id := "bilibili:BV1QraJ6ZESK"
	job, err := s.Start(context.Background(), id, false)
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, s, job.ID, "failed")
	job, _ = s.Get(job.ID)
	if job.ErrorCode != "model_memory" || job.FailedStage != "recognizing" || !strings.Contains(job.Error, "内存不足") {
		t.Fatal(job)
	}
	tracks, _ := saved.List(context.Background(), id)
	if len(tracks) != 0 {
		t.Fatal("failed transcript saved")
	}
}
func waitJob(t *testing.T, s *transcription.Service, id string, state string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, _ := s.Get(id)
		if job.State == state {
			return
		}
		if job.State == "failed" && state != "failed" {
			t.Fatal(job.Error)
		}
		time.Sleep(5 * time.Millisecond)
	}
	job, _ := s.Get(id)
	t.Fatalf("state %s, wanted %s", job.State, state)
}
func setup(t *testing.T, engine testEngine) (*transcription.Service, *subtitles.SavedService) {
	t.Helper()
	root := t.TempDir()
	db, err := sqlite.Open(filepath.Join(root, "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	saved := subtitles.NewSavedService(db)
	s, err := transcription.NewWithStore(filesystem.NewTranscriptionSettings(filepath.Join(root, "settings.json")), transcription.Config{Binary: "fixture-cli", Model: "fixture.bin", Language: "auto"}, engine, func(context.Context, string) (transcription.Audio, error) {
		return transcription.Audio{URL: "https://media.xyzcdn.net/fixture.mp3"}, nil
	}, saved)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, saved
}
func TestGenerationIsExplicitDurableAndCanceledWorkWritesNothing(t *testing.T) {
	id := "rss:" + strings.Repeat("a", 64)
	s, saved := setup(t, testEngine{})
	if s.Current(id) != nil {
		t.Fatal("job ran automatically")
	}
	job, err := s.Start(context.Background(), id, true)
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, s, job.ID, "completed")
	values, err := saved.List(context.Background(), id)
	if err != nil || len(values) != 1 || values[0].Track.Language != "whisper-preview" {
		t.Fatal("result not saved", err)
	}
	gate := make(chan struct{})
	s2, saved2 := setup(t, testEngine{gate: gate})
	job, err = s2.Start(context.Background(), id, false)
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, s2, job.ID, "recognizing")
	if err = s2.Cancel(job.ID); err != nil {
		t.Fatal(err)
	}
	waitJob(t, s2, job.ID, "canceled")
	values, _ = saved2.List(context.Background(), id)
	if len(values) != 0 {
		t.Fatal("canceled result persisted")
	}
}
func TestConcurrentCaptionEditIsPreserved(t *testing.T) {
	id := "xiaoyuzhou:" + strings.Repeat("b", 24)
	gate := make(chan struct{})
	s, saved := setup(t, testEngine{gate: gate})
	job, err := s.Start(context.Background(), id, false)
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, s, job.ID, "recognizing")
	_, err = saved.Save(context.Background(), id, subtitles.Track{Language: "whisper", Label: "外部修改", Cues: []subtitles.Cue{{Start: 1, End: 3, Text: "保留外部版本"}}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	close(gate)
	waitJob(t, s, job.ID, "failed")
	values, _ := saved.List(context.Background(), id)
	if values[0].Track.Cues[0].Text != "保留外部版本" {
		t.Fatal("concurrent version overwritten")
	}
}

func TestVideoIDsUseTheSameExplicitJobAndPersistenceContract(t *testing.T) {
	for _, id := range []string{"bilibili:BV17x411w7KC", "youtube:jNQXAC9IVRw"} {
		s, saved := setup(t, testEngine{})
		job, err := s.Start(context.Background(), id, true)
		if err != nil {
			t.Fatal(err)
		}
		waitJob(t, s, job.ID, "completed")
		tracks, err := saved.List(context.Background(), id)
		if err != nil || len(tracks) != 1 {
			t.Fatal("video transcript missing", err)
		}
	}
}

func TestFirstUseModelDownloadProgressIsVisibleOnTheJob(t *testing.T) {
	gate := make(chan struct{})
	s, _ := setup(t, testEngine{gate: gate, download: true})
	job, err := s.Start(context.Background(), "bilibili:BV1QraJ6ZESK", true)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		current, _ := s.Get(job.ID)
		if current.State == "model" && current.Received == 50 && current.Total == 100 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("download progress not visible", current)
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(gate)
	waitJob(t, s, job.ID, "completed")
	if current, _ := s.Get(job.ID); current.Received != 0 || current.Total != 0 {
		t.Fatal("download progress outlived its stage", current)
	}
}
