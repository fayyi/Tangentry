// Package transcription owns explicit, cancellable speech recognition.
// Engines, platform lookup and subtitle persistence are injected independently.
package transcription

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"tangentry/internal/subtitles"
)

var ErrInvalid = errors.New("invalid transcription input")
var ErrUnavailable = errors.New("transcription unavailable")
var ErrNotReady = errors.New("transcription tools not ready")
var ErrBusy = errors.New("transcription busy")
var ErrNotFound = errors.New("transcription not found")
var ErrUploadConsent = fmt.Errorf("transcription provider upload consent: %w", ErrInvalid)

type task struct {
	job      Job
	cancel   context.CancelFunc
	track    subtitles.Track
	hasTrack bool
}
type Service struct {
	mu      sync.Mutex
	jobs    map[string]*task
	order   []string
	active  string
	closed  bool
	wait    sync.WaitGroup
	config  Config
	store   ConfigStore
	engine  Engine
	resolve SourceResolver
	saved   SavedSubtitles
	// observer receives each job as it starts and once it ends, outside mu.
	observer func(Job)
}

// Observe receives each job as it starts and once it ends. It must not block.
func (s *Service) Observe(observer func(Job)) {
	s.mu.Lock()
	s.observer = observer
	s.mu.Unlock()
}

// NewWithStore constructs the service around an injected settings port.
func NewWithStore(store ConfigStore, initial Config, engine Engine, resolve SourceResolver, saved SavedSubtitles) (*Service, error) {
	loaded, err := store.Load(initial)
	if err != nil {
		return nil, err
	}
	initial = loaded
	if initial.Backend == "" {
		initial.Backend = "whisper.cpp"
	}
	return &Service{jobs: make(map[string]*task), store: store, config: initial, engine: engine, resolve: resolve, saved: saved}, nil
}
func (s *Service) Status() Status {
	return s.status(s.Configuration())
}

// StreamingReady reports whether the current configured engine can accept the
// short local PCM frames used by automatic subtitles. Only whisper.cpp has a
// streaming adapter today; other local engines remain available through the
// established batch transcription flow.
func (s *Service) StreamingReady() bool {
	s.mu.Lock()
	if s.closed || s.config.Cloud() || s.config.Backend != "whisper.cpp" {
		s.mu.Unlock()
		return false
	}
	config := s.config
	engine := s.engine
	s.mu.Unlock()
	if _, ok := engine.(StreamingEngine); !ok {
		return false
	}
	return engine.Status(config).Ready
}
func (s *Service) status(c Config) Status {
	result := s.engine.Status(c)
	result.GroqKeyConfigured, result.CloudflareTokenConfigured = c.GroqAPIKey != "", c.CloudflareAPIToken != ""
	result.Config.GroqAPIKey, result.Config.CloudflareAPIToken = "", ""
	result.SiliconflowKeyConfigured = c.SiliconflowAPIKey != ""
	result.Config.SiliconflowAPIKey = ""
	result.MAICookieConfigured = c.MAICookie != ""
	result.Config.MAICookie = ""
	return result
}
func (s *Service) Configuration() Config {
	s.mu.Lock()
	c := s.config
	s.mu.Unlock()
	return c
}
func (s *Service) Configure(c Config) (Status, error) {
	return s.ConfigureSettings(SettingsInput{Config: c})
}
func (s *Service) ConfigureSettings(input SettingsInput) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.configureSettings(input)
}

// Block new recognition and settings writes while relocating managed model files.
func (s *Service) WithIdleModels(action func(Config) (Config, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.active != "" {
		return ErrBusy
	}
	c, err := action(s.config)
	if err != nil {
		return err
	}
	_, err = s.configureSettings(SettingsInput{Config: c})
	return err
}

func (s *Service) configureSettings(input SettingsInput) (Status, error) {
	c := input.Config
	c.GroqAPIKey, c.CloudflareAPIToken = s.config.GroqAPIKey, s.config.CloudflareAPIToken
	c.SiliconflowAPIKey = s.config.SiliconflowAPIKey
	c.MAICookie = s.config.MAICookie
	if input.GroqAPIKey != nil {
		c.GroqAPIKey = strings.TrimSpace(*input.GroqAPIKey)
	}
	if input.CloudflareAPIToken != nil {
		c.CloudflareAPIToken = strings.TrimSpace(*input.CloudflareAPIToken)
	}
	if input.SiliconflowAPIKey != nil {
		c.SiliconflowAPIKey = strings.TrimSpace(*input.SiliconflowAPIKey)
	}
	if input.MAICookie != nil {
		c.MAICookie = strings.TrimSpace(*input.MAICookie)
	}
	if !validConfig(c) {
		return Status{}, ErrInvalid
	}
	if err := s.store.Save(c); err != nil {
		return Status{}, ErrUnavailable
	}
	s.config = c
	return s.status(c), nil
}
