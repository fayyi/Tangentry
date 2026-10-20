// Package subtitles owns explicit reading of existing captions, independently of media downloads.
package subtitles

import (
	"context"
	"errors"
	"sync"
	"time"

	"tangentry/internal/discovery"
)

var (
	ErrInvalid           = errors.New("invalid subtitle query")
	ErrTools             = errors.New("subtitle tool unavailable")
	ErrBusy              = errors.New("subtitle reader busy")
	ErrUnavailable       = errors.New("subtitle retrieval failed")
	ErrPublicUnavailable = errors.New("public podcast subtitle retrieval failed")
	ErrFormat            = errors.New("invalid subtitle data")
	ErrLogin             = errors.New("subtitle access requires login")
	ErrRestricted        = errors.New("subtitle request restricted")
)

type Cue struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

type Track struct {
	Language  string `json:"language"`
	Label     string `json:"label"`
	Automatic bool   `json:"automatic"`
	Source    string `json:"source,omitempty"`
	Mismatch  bool   `json:"mismatch,omitempty"`
	Cues      []Cue  `json:"cues"`
}

type Language struct {
	Language  string `json:"language"`
	Label     string `json:"label"`
	Automatic bool   `json:"automatic"`
	Source    string `json:"source,omitempty"`
	HasURL    bool   `json:"hasUrl"`
}

type Part struct {
	CID          int64      `json:"cid"`
	Page         int        `json:"page"`
	Title        string     `json:"title,omitempty"`
	Status       string     `json:"status"`
	ErrorCode    int        `json:"errorCode,omitempty"`
	ErrorMessage string     `json:"errorMessage,omitempty"`
	Languages    []Language `json:"languages"`
	Tracks       []Track    `json:"tracks"`
}

type Document struct {
	Languages    []Language `json:"languages,omitempty"`
	Parts        []Part     `json:"parts,omitempty"`
	VideoID      string     `json:"videoId"`
	Part         int        `json:"part"`
	Status       string     `json:"status"` // available, none, login_required, api_empty, error, cc, ai, ai_mismatch
	ErrorCode    int        `json:"errorCode,omitempty"`
	ErrorMessage string     `json:"errorMessage,omitempty"`
	Tracks       []Track    `json:"tracks"`
}

// Providers return only plain captions; tool payloads, credentials and media URLs stay outside the contract.
type Provider interface {
	ReadSubtitles(context.Context, string) (Document, error)
}

type YouTubeProvider interface {
	ReadYouTubeSubtitles(context.Context, string, string) (Document, error)
}

type Service struct {
	provider Provider
	slots    chan struct{}
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	closed   bool
	active   sync.WaitGroup
}

func NewService(provider Provider) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{provider: provider, slots: make(chan struct{}, 2), ctx: ctx, cancel: cancel}
}

func (s *Service) Read(ctx context.Context, query string) (Document, error) {
	kind, id, err := discovery.ParseQuery(query)
	if err != nil || kind != "video" || len(query) > 500 {
		return Document{}, ErrInvalid
	}
	return s.withReader(ctx, func(ctx context.Context) (Document, error) { return s.provider.ReadSubtitles(ctx, id) })
}

func (s *Service) ReadYouTube(ctx context.Context, query, language string) (Document, error) {
	id, _, err := discovery.ParseYouTube(query)
	if err != nil || len(query) > 500 || language != "" && !savedLanguage.MatchString(language) {
		return Document{}, ErrInvalid
	}
	provider, ok := s.provider.(YouTubeProvider)
	if !ok {
		return Document{}, ErrTools
	}
	return s.withReader(ctx, func(ctx context.Context) (Document, error) { return provider.ReadYouTubeSubtitles(ctx, id, language) })
}

func (s *Service) withReader(ctx context.Context, read func(context.Context) (Document, error)) (Document, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return Document{}, ErrUnavailable
	}
	s.active.Add(1)
	s.mu.Unlock()
	defer s.active.Done()
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return Document{}, ErrBusy
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	return read(ctx)
}

func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cancel()
	s.active.Wait()
}
