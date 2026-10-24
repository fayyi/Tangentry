package transcription

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"time"

	"tangentry/internal/content"
	"tangentry/internal/subtitles"
)

func (s *Service) Start(ctx context.Context, id string, preview bool) (Job, error) {
	return s.StartWithUploadConsent(ctx, id, preview, false, "")
}

// StartAutomatic is a local-only entry point for the automatic subtitle
// coordinator. It shares the existing resolver and ASR engine without using
// cloud credentials or changing the explicit transcription API.
func (s *Service) StartAutomatic(ctx context.Context, id, language string) (Job, error) {
	return s.start(ctx, id, false, false, "", true, language)
}

// RecognizePCM delegates a short decoded chunk to a local streaming engine.
// Cloud backends are explicitly rejected even if configured for manual jobs.
func (s *Service) RecognizePCM(ctx context.Context, language string, pcm []byte, offset float64) (subtitles.Track, error) {
	s.mu.Lock()
	if s.closed || s.config.Cloud() {
		s.mu.Unlock()
		return subtitles.Track{}, ErrUploadConsent
	}
	config := s.config
	engine, ok := s.engine.(StreamingEngine)
	s.mu.Unlock()
	if !ok {
		return subtitles.Track{}, ErrUnavailable
	}
	if language != "" {
		config.Language = language
	}
	return engine.RecognizePCM(ctx, config, pcm, offset)
}

func (s *Service) StartWithUploadConsent(ctx context.Context, id string, preview, upload bool, backend string) (Job, error) {
	return s.start(ctx, id, preview, upload, backend, false, "")
}

func (s *Service) start(ctx context.Context, id string, preview, upload bool, backend string, automatic bool, languageOverride string) (Job, error) {
	if !content.SpeechID(id) {
		return Job{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Job{}, ErrUnavailable
	}
	if (automatic && s.config.Cloud()) || (backend != "" && backend != s.config.Backend) || (s.config.Cloud() && (!upload || backend != s.config.Backend)) {
		return Job{}, ErrUploadConsent
	}
	if s.active != "" {
		current := s.jobs[s.active].job
		if current.ContentID == id && current.Preview == preview && current.Automatic == automatic {
			return current, nil
		}
		return Job{}, ErrBusy
	}
	if !s.engine.Status(s.config).Ready {
		return Job{}, ErrNotReady
	}
	// Capture revisions before work begins. Never overwrite a concurrent edit.
	language := jobLanguage(s.config, preview)
	if automatic && languageOverride != "" {
		language = languageOverride
	}
	tracks, err := s.saved.List(ctx, id)
	if err != nil {
		return Job{}, err
	}
	var revision int64
	for _, track := range tracks {
		if track.Track.Language == language {
			revision = track.Revision
		}
	}
	var rawID [16]byte
	if _, err := rand.Read(rawID[:]); err != nil {
		return Job{}, ErrUnavailable
	}
	token := hex.EncodeToString(rawID[:])
	job := Job{ID: token, ContentID: id, State: "resolving", Preview: preview, Backend: s.config.Backend, Automatic: automatic}
	work, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	s.jobs[token] = &task{job: job, cancel: cancel}
	s.active = token
	s.order = append(s.order, token)
	for len(s.order) > 20 {
		delete(s.jobs, s.order[0])
		s.order = s.order[1:]
	}
	cfg := s.config
	s.wait.Add(1)
	go s.run(work, token, cfg, revision, language, automatic)
	return job, nil
}

func jobLanguage(config Config, preview bool) string {
	language := "whisper"
	if preview {
		language = "whisper-preview"
	}
	if config.Backend == "siliconflow" {
		language = "siliconflow"
		if preview {
			language += "-preview"
		}
	}
	return language
}

func (s *Service) update(id, state, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current := s.jobs[id]; current != nil {
		if current.job.State != state {
			current.job.Received, current.job.Total = 0, 0
		}
		current.job.State = state
		current.job.Error = message
	}
}

func (s *Service) run(ctx context.Context, id string, cfg Config, revision int64, language string, automatic bool) {
	defer s.wait.Done()
	s.mu.Lock()
	current := s.jobs[id]
	job := current.job
	observer := s.observer
	s.mu.Unlock()
	if observer != nil {
		observer(job)
	}
	defer func() {
		current.cancel()
		s.mu.Lock()
		if s.active == id {
			s.active = ""
		}
		finished := current.job
		s.mu.Unlock()
		if observer != nil {
			observer(finished)
		}
	}()
	ctx = WithProgress(ctx, func(received, total int64) {
		s.mu.Lock()
		current.job.Received, current.job.Total = received, total
		s.mu.Unlock()
	})
	audio, err := s.resolve(ctx, job.ContentID)
	var track subtitles.Track
	if err == nil {
		limit := 0
		if job.Preview {
			limit = 60
		}
		track, err = s.engine.Transcribe(ctx, cfg, audio, limit, func(stage string) { s.update(id, stage, "") })
	}
	if err == nil && ctx.Err() == nil {
		track.Language = language
		if automatic {
			track.Automatic = true
			track.Source = "automatic"
			s.mu.Lock()
			current.track, current.hasTrack = track, true
			s.mu.Unlock()
		} else {
			s.update(id, "saving", "")
			_, err = s.saved.Save(ctx, job.ContentID, track, revision)
		}
	}
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			s.update(id, "failed", "转写超时，请先尝试转写前一分钟。")
		} else {
			s.update(id, "canceled", "")
		}
		return
	}
	if err != nil {
		message := "转写未完成，请检查音频、工具与磁盘空间后重试。"
		code := "transcription_failed"
		s.mu.Lock()
		stage := current.job.State
		s.mu.Unlock()
		if stage == "resolving" {
			message, code = "未能获取媒体音轨，请检查平台登录配置与网络后重试。", "source_unavailable"
		}
		var failure *Failure
		if errors.As(err, &failure) {
			message, code = failure.Message, failure.Code
		} else if errors.Is(err, context.DeadlineExceeded) && stage == "fetching" {
			message, code = "音频暂存超过 15 分钟，请检查网络或尝试较小的音频源。", "audio_timeout"
		} else if errors.Is(err, subtitles.ErrFormat) {
			message, code = "识别结果为空或字幕时间无效，请确认音轨有人声，或更换模型后重试。", "invalid_transcript"
		} else if errors.Is(err, ErrNotReady) {
			message, code = "转录工具或模型不可用，请检查工具设置。", "tools_not_ready"
		}
		if errors.Is(err, subtitles.ErrSavedConflict) {
			message = "转写期间字幕已在其他页面修改，未覆盖原字幕。"
			code = "subtitle_conflict"
		}
		s.mu.Lock()
		current.job.FailedStage, current.job.ErrorCode = stage, code
		current.job.State, current.job.Error = "failed", message
		s.mu.Unlock()
		slog.Warn("transcription failed", "job", id, "content", job.ContentID, "stage", stage, "code", code)
		return
	}
	s.update(id, "completed", "")
}

// Result returns an automatic job's in-memory result. Explicit jobs continue
// to expose their formal result through the saved subtitle store.
func (s *Service) Result(id string) (subtitles.Track, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value := s.jobs[id]
	if value == nil || !value.hasTrack {
		return subtitles.Track{}, false
	}
	return value.track, true
}

func (s *Service) Get(id string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.jobs[id]
	if !ok {
		return Job{}, ErrNotFound
	}
	return value.job, nil
}

func (s *Service) Current(id string) *Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.order) - 1; i >= 0; i-- {
		value := s.jobs[s.order[i]].job
		if value.ContentID == id {
			return &value
		}
	}
	return nil
}

func (s *Service) Cancel(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.jobs[id]
	if !ok {
		return ErrNotFound
	}
	value.cancel()
	return nil
}

func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	for _, value := range s.jobs {
		value.cancel()
	}
	s.mu.Unlock()
	s.wait.Wait()
	if closer, ok := s.engine.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}
