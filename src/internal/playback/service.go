// Package playback supplies short-lived online playback without creating a
// download, library entry or persistent media file. Upstream URLs stay here.
package playback

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalid     = errors.New("invalid playback request")
	ErrUnavailable = errors.New("online playback unavailable")
	ErrExpired     = errors.New("playback session expired")
	ErrBusy        = errors.New("online playback busy")
)

type Source struct {
	URL      string
	Quality  int
	Duration int
	Backups  []string
}
type Session struct {
	ID      string `json:"id"`
	Quality int    `json:"quality"`
}
type entry struct {
	source        Source
	expires       time.Time
	videoID       string
	sourceExpires time.Time
	revision      uint64
	refresh       chan struct{}
	refreshAfter  time.Time
	mediaSize     int64
}
type Service struct {
	resolve   func(context.Context, string) (Source, error)
	http      *http.Client
	mu        sync.Mutex
	sessions  map[string]entry
	opening   chan struct{}
	streaming chan struct{}
}

// No arbitrary URL input: only a validated video identifier is resolved by the
// platform adapter, and only public Bili CDN addresses may be fetched.
func allowed(raw string) bool {
	if len(raw) > 8192 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return strings.HasSuffix(host, ".bilivideo.com") || strings.HasSuffix(host, ".bilivideo.cn")
}

func New(resolve func(context.Context, string) (Source, error), client *http.Client) *Service {
	clone := *client
	clone.Jar = nil   // API login cookies must never be attached to a CDN request.
	clone.Timeout = 0 // Media bodies can outlive the metadata client's timeout.
	check := client.CheckRedirect
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !allowed(req.URL.String()) {
			return ErrUnavailable
		}
		req.Header.Del("Cookie")
		req.Header.Del("Authorization")
		if check != nil {
			return check(req, via)
		}
		return nil
	}
	return &Service{resolve: resolve, http: &clone, sessions: make(map[string]entry), opening: make(chan struct{}, 2), streaming: make(chan struct{}, 4)}
}

var videoID = regexp.MustCompile(`^BV[0-9A-Za-z]{10}$`)

func (s *Service) Open(ctx context.Context, id string) (Session, error) {
	if !videoID.MatchString(id) {
		return Session{}, ErrInvalid
	}
	select {
	case s.opening <- struct{}{}:
		defer func() { <-s.opening }()
	default:
		return Session{}, ErrBusy
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	source, err := s.resolve(ctx, id)
	if err != nil {
		return Session{}, err
	}
	source, err = normalizeSource(source)
	if err != nil {
		return Session{}, err
	}
	if ctx.Err() != nil {
		return Session{}, ctx.Err()
	}
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return Session{}, ErrUnavailable
	}
	token := hex.EncodeToString(bytes[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for key, value := range s.sessions {
		if now.After(value.expires) {
			delete(s.sessions, key)
		}
	}
	// Active readers retain their streams; opening more fails instead of evicting them.
	if len(s.sessions) >= 32 {
		return Session{}, ErrBusy
	}
	sourceExpires := sourceDeadline(source.URL)
	if !sourceExpires.IsZero() && !sourceExpires.After(now) {
		return Session{}, ErrUnavailable
	}
	s.sessions[token] = entry{source: source, expires: now.Add(2 * time.Hour), sourceExpires: sourceExpires, videoID: id, refresh: make(chan struct{}, 1)}
	return Session{ID: token, Quality: source.Quality}, nil
}

func (s *Service) Release(token string) { s.mu.Lock(); delete(s.sessions, token); s.mu.Unlock() }

var singleRange = regexp.MustCompile(`^bytes=(?:[0-9]+-[0-9]*|-[0-9]+)$`)

// Fetch returns an upstream body and a release function. Both must be closed by
// the transport. Cancellation propagates into the CDN request, including seeks.
func (s *Service) Fetch(ctx context.Context, token, method, byteRange string) (*http.Response, func(), error) {
	if (method != http.MethodGet && method != http.MethodHead) || !validRangeInput(byteRange) {
		return nil, nil, ErrInvalid
	}
	s.mu.Lock()
	value, ok := s.sessions[token]
	if ok && time.Now().After(value.expires) {
		delete(s.sessions, token)
		ok = false
	}
	s.mu.Unlock()
	if !ok {
		return nil, nil, ErrExpired
	}
	select {
	case s.streaming <- struct{}{}:
	default:
		return nil, nil, ErrBusy
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	release := func() { cancel(); <-s.streaming }
	refreshed := false
	if !value.sourceExpires.IsZero() && !value.sourceExpires.After(time.Now().Add(5*time.Second)) {
		var err error
		value, err = s.refreshSession(ctx, token, value)
		if err != nil {
			release()
			return nil, nil, err
		}
		refreshed = true
	}
	var response *http.Response
	for {
		signatureFailure := false
		sources := append([]string{value.source.URL}, value.source.Backups...)
		for _, raw := range sources {
			candidate, closeAttempt, err := s.fetchCandidate(ctx, method, raw, byteRange)
			if err == nil {
				if validMediaResponse(candidate, byteRange) && s.rememberSource(token, value.revision, raw, mediaSize(candidate)) {
					response = candidate
					// Closing the body releases this attempt; the caller also cancels
					// the complete stream through release(). No bytes are spliced.
					response.Body = &cancelBody{ReadCloser: response.Body, cancel: closeAttempt}
					break
				}
				if candidate.StatusCode == 403 || candidate.StatusCode == 410 {
					signatureFailure = true
				}
				candidate.Body.Close()
			}
			closeAttempt()
			if ctx.Err() != nil {
				break
			}
		}
		if response == nil && refreshed && signatureFailure {
			s.mu.Lock()
			if current, ok := s.sessions[token]; ok && current.revision == value.revision {
				current.refreshAfter = time.Now().Add(30 * time.Second)
				s.sessions[token] = current
			}
			s.mu.Unlock()
		}
		if response != nil || ctx.Err() != nil || refreshed || !signatureFailure {
			break
		}
		var err error
		value, err = s.refreshSession(ctx, token, value)
		if err != nil {
			release()
			return nil, nil, err
		}
		refreshed = true
	}
	if response == nil {
		contextErr := ctx.Err()
		release()
		if contextErr != nil {
			return nil, nil, contextErr
		}
		return nil, nil, ErrUnavailable // Signed URLs never appear in errors/logs.
	}
	return response, release, nil
}

// ValidSource checks platform media boundaries for app-level composition.
func ValidSource(source Source) bool { return allowed(source.URL) }
