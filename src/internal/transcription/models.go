package transcription

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Model downloads are independent of recognition. Reading settings or starting
// transcription never calls the installer, and installation never changes config.
type ModelOption struct {
	Backend     string `json:"backend,omitempty"`
	ID          string `json:"id"`
	Size        string `json:"size"`
	Description string `json:"description"`
	Repository  string `json:"repository"`
	Installed   bool   `json:"installed"`
	Path        string `json:"path,omitempty"`
	Available   bool   `json:"available"`
}

func ModelCatalog() []ModelOption {
	return []ModelOption{
		{ID: "whisper-tiny", Backend: "whisper.cpp", Size: "约 75 MB", Description: "轻量，先试用", Repository: "ggerganov/whisper.cpp"},
		{ID: "whisper-base", Backend: "whisper.cpp", Size: "约 145 MB", Description: "入门，资源占用较低", Repository: "ggerganov/whisper.cpp"},
		{ID: "whisper-small", Backend: "whisper.cpp", Size: "约 485 MB", Description: "平衡大小与识别效果", Repository: "ggerganov/whisper.cpp"},
		{ID: "whisper-medium", Backend: "whisper.cpp", Size: "约 1.5 GB", Description: "更大，需要更多内存", Repository: "ggerganov/whisper.cpp"},
		{ID: "whisper-large-v3-turbo", Backend: "whisper.cpp", Size: "约 1.6 GB", Description: "高阶，兼顾速度", Repository: "ggerganov/whisper.cpp"},
		{ID: "whisper-large-v3", Backend: "whisper.cpp", Size: "约 3.1 GB", Description: "完整大模型，需要更多内存", Repository: "ggerganov/whisper.cpp"},
		{ID: "qwen3-asr-0.6b", Backend: "qwen3-asr", Size: "约 1.2 GB", Description: "本机识别 · 轻量", Repository: "Qwen/Qwen3-ASR-0.6B"},
		{ID: "qwen3-asr-1.7b", Backend: "qwen3-asr", Size: "约 3.4 GB", Description: "本机识别 · 更大模型", Repository: "Qwen/Qwen3-ASR-1.7B"},
		{ID: "qwen3-aligner-0.6b", Backend: "qwen3-asr", Size: "约 1.2 GB", Description: "字幕时间对齐 · 必需", Repository: "Qwen/Qwen3-ForcedAligner-0.6B"},
		{ID: "tiny", Size: "约 75 MB", Description: "轻量，先试用", Repository: "Systran/faster-whisper-tiny"},
		{ID: "base", Size: "约 145 MB", Description: "入门，资源占用较低", Repository: "Systran/faster-whisper-base"},
		{ID: "small", Size: "约 485 MB", Description: "平衡大小与识别效果", Repository: "Systran/faster-whisper-small"},
		{ID: "medium", Size: "约 1.5 GB", Description: "更大，需要更多内存", Repository: "Systran/faster-whisper-medium"},
		{ID: "large-v3-turbo", Size: "约 1.6 GB", Description: "高阶，兼顾速度", Repository: "mobiuslabsgmbh/faster-whisper-large-v3-turbo"},
		{ID: "large-v3", Size: "约 3.1 GB", Description: "完整大模型，CPU 耗时更长", Repository: "Systran/faster-whisper-large-v3"},
	}
}

type ModelInstaller interface {
	Available(string) bool
	Installed(ModelOption) (string, bool)
	Install(context.Context, ModelOption, string, func(string)) error
}
type ModelDownload struct {
	ID       string `json:"id"`
	ModelID  string `json:"modelId"`
	State    string `json:"state"`
	Error    string `json:"error,omitempty"`
	Received int64  `json:"received,omitempty"`
	Total    int64  `json:"total,omitempty"`
}
type ModelDownloads struct {
	mu        sync.Mutex
	wait      sync.WaitGroup
	installer ModelInstaller
	python    func() string
	job       *ModelDownload
	cancel    context.CancelFunc
	closed    bool
}

func NewModelDownloads(installer ModelInstaller, python func() string) *ModelDownloads {
	return &ModelDownloads{installer: installer, python: python}
}
func (s *ModelDownloads) available(option ModelOption, python string) bool {
	if installer, ok := s.installer.(interface {
		AvailableFor(ModelOption, string) bool
	}); ok {
		return installer.AvailableFor(option, python)
	}
	return s.installer.Available(python)
}
func (s *ModelDownloads) Status() ([]ModelOption, *ModelDownload, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	options := ModelCatalog()
	python := s.python()
	availability := map[string]bool{}
	for i := range options {
		options[i].Path, options[i].Installed = s.installer.Installed(options[i])
		available, checked := availability[options[i].Backend]
		if !checked {
			available = s.available(options[i], python)
			availability[options[i].Backend] = available
		}
		options[i].Available = available
	}
	var job *ModelDownload
	if s.job != nil {
		copy := *s.job
		job = &copy
	}
	return options, job, availability[""]
}
func (s *ModelDownloads) WithIdle(action func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.cancel != nil {
		return ErrBusy
	}
	return action()
}
func (s *ModelDownloads) Start(id string) (ModelDownload, error) {
	var option *ModelOption
	for _, candidate := range ModelCatalog() {
		if candidate.ID == id {
			value := candidate
			option = &value
			break
		}
	}
	if option == nil {
		return ModelDownload{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ModelDownload{}, ErrUnavailable
	}
	if s.cancel != nil {
		return ModelDownload{}, ErrBusy
	}
	if _, exists := s.installer.Installed(*option); exists {
		return ModelDownload{ModelID: id, State: "completed"}, nil
	}
	python := s.python()
	if !s.available(*option, python) {
		return ModelDownload{}, ErrNotReady
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return ModelDownload{}, ErrUnavailable
	}
	job := ModelDownload{ID: hex.EncodeToString(token[:]), ModelID: id, State: "fetching"}
	s.job = &job
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	s.cancel = cancel
	s.wait.Add(1)
	ctx = WithProgress(ctx, func(received, total int64) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.job.Received, s.job.Total = received, total
	})
	go func() {
		defer s.wait.Done()
		defer cancel()
		err := s.installer.Install(ctx, *option, python, func(state string) {
			s.mu.Lock()
			defer s.mu.Unlock()
			if ctx.Err() == nil {
				s.job.State = state
			}
		})
		s.mu.Lock()
		defer s.mu.Unlock()
		if ctx.Err() != nil {
			s.job.State = "canceled"
			if ctx.Err() == context.DeadlineExceeded {
				s.job.State = "failed"
				s.job.Error = "模型下载超时，请检查网络后重试。"
			}
		} else if err != nil {
			s.job.State = "failed"
			s.job.Error = "模型未能完整下载，请检查网络与磁盘空间后重试。"
		} else {
			s.job.State = "completed"
		}
		s.cancel = nil
	}()
	return job, nil
}
func (s *ModelDownloads) Cancel(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job == nil || s.job.ID != id {
		return ErrNotFound
	}
	if s.cancel != nil {
		s.job.State = "cancelling"
		s.cancel()
	}
	return nil
}
func (s *ModelDownloads) Close() {
	s.mu.Lock()
	s.closed = true
	if s.cancel != nil {
		s.job.State = "cancelling"
		s.cancel()
	}
	s.mu.Unlock()
	s.wait.Wait()
}
