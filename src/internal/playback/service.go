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
	URL        string
	Quality    int
	Duration   int
	Protocol   string
	Extension  string
	VideoCodec string
	AudioCodec string
	Backups    []string
	Platform   string
	Referer    string
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
	diagnostics   *DiagnosticTrace
}
type Service struct {
	resolve   func(context.Context, string) (Source, error)
	http      *http.Client
	mu        sync.Mutex
	sessions  map[string]entry
	opening   chan struct{}
	streaming chan struct{}
}

func (s *Service) OpenDouyin(ctx context.Context, id string) (Session, error) {
	trace := DiagnosticsFromContext(ctx)
	if trace == nil {
		trace = NewDiagnosticTrace()
		ctx = ContextWithDiagnostics(ctx, trace)
	}
	if !ValidDouyinID(id) {
		trace.Add(DiagnosticStage{Stage: "playback_open", Outcome: OutcomeException, Reason: stringPointer("invalid_id")})
		return Session{}, WrapDiagnostic(ErrInvalid, trace, "抖音视频标识无效。")
	}
	trace.Add(DiagnosticStage{Stage: "playback_open", Outcome: OutcomeOK, Reason: stringPointer("id_validated")})
	session, err := s.open(ctx, id)
	if err != nil {
		outcome := OutcomeForError(err)
		trace.Add(DiagnosticStage{Stage: "playback_open", Outcome: outcome, Reason: stringPointer(openFailureReason(err))})
		var diagnostic *DiagnosticError
		if errors.As(err, &diagnostic) {
			return Session{}, err
		}
		return Session{}, WrapDiagnostic(err, trace, "抖音视频解析失败，请在详情中查看脱敏阶段信息。")
	}
	trace.Add(DiagnosticStage{Stage: "playback_open", Outcome: OutcomeOK, Reason: stringPointer("resolver_success")})
	if _, ok := s.getEntry(session.ID); ok {
		s.mu.Lock()
		if current, exists := s.sessions[session.ID]; exists {
			current.diagnostics = trace
			s.sessions[session.ID] = current
		}
		s.mu.Unlock()
	}
	return session, nil
}

func stringPointer(value string) *string { return &value }

func openFailureReason(err error) string {
	switch {
	case errors.Is(err, ErrBusy):
		return "concurrency_limit"
	case errors.Is(err, context.DeadlineExceeded):
		return "resolver_timeout"
	case errors.Is(err, ErrInvalid):
		return "invalid_id"
	default:
		return "resolver_error"
	}
}

// OpenBilibili keeps the Bilibili HTTP route from accepting identifiers that
// belong to another platform. Open remains as an internal compatibility entry
// point for callers that still dispatch by identifier prefix.
func (s *Service) OpenBilibili(ctx context.Context, id string) (Session, error) {
	if !ValidBilibiliID(id) {
		return Session{}, ErrInvalid
	}
	return s.open(ctx, id)
}

func sourceReferer(platform, id string) string {
	if platform == "douyin" {
		return "https://www.douyin.com/video/" + id
	}
	return "https://www.bilibili.com/"
}

// No arbitrary URL input: only a validated video identifier is resolved by the
// platform adapter, and only public Bili CDN addresses may be fetched.
func allowed(raw string) bool { return allowedFor("bilibili", raw) }
func allowedFor(platform, raw string) bool {
	if len(raw) > 8192 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if platform == "douyin" {
		return host == "douyinvod.com" || strings.HasSuffix(host, ".douyinvod.com")
	}
	return strings.HasSuffix(host, ".bilivideo.com") || strings.HasSuffix(host, ".bilivideo.cn")
}

func New(resolve func(context.Context, string) (Source, error), client *http.Client) *Service {
	clone := *client
	clone.Jar = nil   // API login cookies must never be attached to a CDN request.
	clone.Timeout = 0 // Media bodies can outlive the metadata client's timeout.
	check := client.CheckRedirect
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return ErrUnavailable
		}
		platform := ""
		if len(via) > 0 {
			platform = mediaPlatform(via[0].URL.String())
		}
		if platform == "" || !allowedFor(platform, req.URL.String()) {
			return ErrUnavailable
		}
		req.Header.Del("Cookie")
		req.Header.Del("Authorization")
		if len(via) > 0 && via[0].Header.Get("Referer") != "" {
			req.Header.Set("Referer", via[0].Header.Get("Referer"))
		}
		if check != nil {
			return check(req, via)
		}
		return nil
	}
	return &Service{resolve: resolve, http: &clone, sessions: make(map[string]entry), opening: make(chan struct{}, 2), streaming: make(chan struct{}, 4)}
}

func mediaPlatform(raw string) string {
	if allowedFor("douyin", raw) {
		return "douyin"
	}
	if allowedFor("bilibili", raw) {
		return "bilibili"
	}
	return ""
}

var videoID = regexp.MustCompile(`^BV[0-9A-Za-z]{10}$`)
var douyinID = regexp.MustCompile(`^[1-9][0-9]{15,19}$`)

func ValidBilibiliID(id string) bool { return videoID.MatchString(id) }
func ValidDouyinID(id string) bool   { return douyinID.MatchString(id) }

func (s *Service) Open(ctx context.Context, id string) (Session, error) {
	if !ValidBilibiliID(id) && !ValidDouyinID(id) {
		return Session{}, ErrInvalid
	}
	return s.open(ctx, id)
}

func (s *Service) open(ctx context.Context, id string) (Session, error) {
	select {
	case s.opening <- struct{}{}:
		defer func() { <-s.opening }()
	default:
		RecordDiagnostic(ctx, DiagnosticStage{Stage: "playback_open", Outcome: OutcomeException, Reason: stringPointer("concurrency_limit")})
		return Session{}, ErrBusy
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	RecordDiagnostic(ctx, DiagnosticStage{Stage: "playback_open", Outcome: OutcomeOK, Reason: stringPointer("resolver_started")})
	source, err := s.resolve(ctx, id)
	if err != nil {
		return Session{}, err
	}
	rawSource := source
	source, err = normalizeSource(source)
	if err != nil {
		outcome, reason := OutcomeNoURL, "media_not_found"
		platform := rawSource.Platform
		if platform == "" {
			platform = "bilibili"
		}
		if rawSource.URL != "" && !allowedFor(platform, rawSource.URL) {
			outcome = OutcomeHTTPError
			reason = "cdn_rejected"
		} else {
			if rawSource.URL != "" && !sourceDeadline(rawSource.URL).IsZero() {
				reason = "source_expired"
			}
		}
		protocol := rawSource.Protocol
		if protocol == "" {
			if parsed, parseErr := url.Parse(rawSource.URL); parseErr == nil {
				protocol = parsed.Scheme
			}
		}
		extension := rawSource.Extension
		if extension == "" {
			extension = mediaExtension(rawSource.URL)
		}
		var deadline *int64
		if rawSource.URL != "" {
			deadline = deadlinePointer(rawSource.URL)
		}
		var cdnAllowed *bool
		if rawSource.URL != "" {
			cdnAllowed = boolPointer(allowedFor(platform, rawSource.URL))
		}
		var videoCodec, audioCodec *string
		if rawSource.VideoCodec != "" {
			videoCodec = stringPointer(rawSource.VideoCodec)
		}
		if rawSource.AudioCodec != "" {
			audioCodec = stringPointer(rawSource.AudioCodec)
		}
		RecordDiagnostic(ctx, DiagnosticStage{Stage: "source_validate", Outcome: outcome, Reason: stringPointer(reason), Protocol: stringPointer(protocol), Extension: stringPointer(extension), VideoCodec: videoCodec, AudioCodec: audioCodec, CDNAllowed: cdnAllowed, DurationSeconds: intPointer(rawSource.Duration), SignatureDeadlineUnix: deadline})
		return Session{}, err
	}
	protocol := source.Protocol
	if protocol == "" {
		protocol = "https"
	}
	extension := source.Extension
	if extension == "" {
		extension = mediaExtension(source.URL)
	}
	var videoCodec, audioCodec *string
	if source.VideoCodec != "" {
		videoCodec = stringPointer(source.VideoCodec)
	}
	if source.AudioCodec != "" {
		audioCodec = stringPointer(source.AudioCodec)
	}
	douyin := source.Platform == "douyin"
	var deadline *int64
	var cdnAllowed *bool
	if douyin {
		deadline = deadlinePointer(source.URL)
		cdnAllowed = boolPointer(ValidSource(source))
	}
	RecordDiagnostic(ctx, DiagnosticStage{Stage: "source_validate", Outcome: OutcomeOK, Reason: stringPointer("source_valid"), Protocol: stringPointer(protocol), Extension: stringPointer(extension), VideoCodec: videoCodec, AudioCodec: audioCodec, CDNAllowed: cdnAllowed, DurationSeconds: intPointer(source.Duration), SignatureDeadlineUnix: deadline})
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
		RecordDiagnostic(ctx, DiagnosticStage{Stage: "playback_open", Outcome: OutcomeException, Reason: stringPointer("concurrency_limit")})
		return Session{}, ErrBusy
	}
	if source.Platform == "" {
		source.Platform = "bilibili"
	}
	if source.Referer == "" {
		source.Referer = sourceReferer(source.Platform, id)
	}
	if !allowedFor(source.Platform, source.URL) {
		RecordDiagnostic(ctx, DiagnosticStage{Stage: "source_validate", Outcome: OutcomeHTTPError, Reason: stringPointer("cdn_rejected"), CDNAllowed: boolPointer(false)})
		return Session{}, ErrUnavailable
	}
	sourceExpires := sourceDeadline(source.URL)
	if !sourceExpires.IsZero() && !sourceExpires.After(now) {
		RecordDiagnostic(ctx, DiagnosticStage{Stage: "source_validate", Outcome: OutcomeNoURL, Reason: stringPointer("source_expired"), CDNAllowed: boolPointer(true), SignatureDeadlineUnix: int64Pointer(sourceExpires.Unix())})
		return Session{}, ErrUnavailable
	}
	s.sessions[token] = entry{source: source, expires: now.Add(2 * time.Hour), sourceExpires: sourceExpires, videoID: id, refresh: make(chan struct{}, 1), diagnostics: DiagnosticsFromContext(ctx)}
	return Session{ID: token, Quality: source.Quality}, nil
}

func boolPointer(value bool) *bool    { return &value }
func intPointer(value int) *int       { return &value }
func int64Pointer(value int64) *int64 { return &value }
func deadlinePointer(raw string) *int64 {
	deadline := sourceDeadline(raw)
	if deadline.IsZero() {
		return nil
	}
	return int64Pointer(deadline.Unix())
}
func mediaExtension(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "unknown"
	}
	parts := strings.Split(strings.TrimSuffix(u.Path, "/"), "/")
	if len(parts) == 0 {
		return "unknown"
	}
	name := strings.Split(parts[len(parts)-1], ".")
	if len(name) < 2 {
		return "unknown"
	}
	return strings.ToLower(name[len(name)-1])
}

func (s *Service) getEntry(token string) (entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.sessions[token]
	return value, ok
}

func (s *Service) Release(token string) {
	s.mu.Lock()
	value, ok := s.sessions[token]
	delete(s.sessions, token)
	s.mu.Unlock()
	if ok && value.source.Platform == "douyin" && value.diagnostics != nil && !value.diagnostics.IsLogged() {
		value.diagnostics.Log(FinalOutcome(nil, value.diagnostics.Snapshot().Stages))
	}
}

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
	trace := value.diagnostics
	if value.source.Platform == "douyin" && trace != nil {
		trace.Add(DiagnosticStage{Stage: "session_media_fetch", Outcome: OutcomeOK, Reason: stringPointer("resolver_started"), Attempted: intPointer(0), Refreshed: boolPointer(false)})
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
			if trace != nil {
				trace.Add(DiagnosticStage{Stage: "session_media_fetch", Outcome: OutcomeForError(err), Reason: stringPointer("signature_refresh"), Refreshed: boolPointer(true)})
			}
			release()
			return nil, nil, s.wrapDouyinFetchError(value, trace, err)
		}
		refreshed = true
	}
	var response *http.Response
	for {
		signatureFailure := false
		signatureFailureStatus := 0
		attempted := 0
		sources := append([]string{value.source.URL}, value.source.Backups...)
		for _, raw := range sources {
			attempted++
			candidate, closeAttempt, err := s.fetchCandidate(ctx, method, raw, byteRange, value.source.Platform, value.source.Referer)
			if candidate != nil && trace != nil && value.source.Platform == "douyin" {
				typeValue := candidate.Header.Get("Content-Type")
				status := candidate.StatusCode
				outcome, reason := OutcomeOK, "source_valid"
				if status >= 400 {
					outcome, reason = OutcomeHTTPError, "candidate_http_error"
				} else if !validMediaResponse(candidate, byteRange) {
					outcome, reason = OutcomeException, "invalid_media_response"
				}
				trace.Add(DiagnosticStage{Stage: "session_media_fetch", Outcome: outcome, HTTPStatus: &status, ContentType: &typeValue, PageURL: ptrString(raw), IsDouyinContext: IsDouyinContext(ptrString(value.source.Referer)), Reason: stringPointer(reason), Attempted: intPointer(attempted), Refreshed: boolPointer(refreshed)})
			}
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
					signatureFailureStatus = candidate.StatusCode
				}
				candidate.Body.Close()
			}
			if err != nil && trace != nil && value.source.Platform == "douyin" {
				outcome, reason := OutcomeException, "network_error"
				if errors.Is(err, context.DeadlineExceeded) {
					outcome, reason = OutcomeTimeout, "timeout"
				}
				trace.Add(DiagnosticStage{Stage: "session_media_fetch", Outcome: outcome, Reason: stringPointer(reason), Attempted: intPointer(attempted), Refreshed: boolPointer(refreshed)})
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
		if trace != nil && value.source.Platform == "douyin" && signatureFailure {
			trace.Add(DiagnosticStage{Stage: "session_media_fetch", Outcome: OutcomeHTTPError, HTTPStatus: intPointer(signatureFailureStatus), Reason: stringPointer("signature_refresh"), Attempted: intPointer(attempted), Refreshed: boolPointer(refreshed)})
		}
		if response != nil || ctx.Err() != nil || refreshed || !signatureFailure {
			break
		}
		var err error
		value, err = s.refreshSession(ctx, token, value)
		if err != nil {
			release()
			return nil, nil, s.wrapDouyinFetchError(value, trace, err)
		}
		refreshed = true
	}
	if response == nil {
		contextErr := ctx.Err()
		release()
		if contextErr != nil {
			return nil, nil, s.wrapDouyinFetchError(value, trace, contextErr)
		}
		return nil, nil, s.wrapDouyinFetchError(value, trace, ErrUnavailable) // Signed URLs never appear in errors/logs.
	}
	return response, release, nil
}

func (s *Service) wrapDouyinFetchError(value entry, trace *DiagnosticTrace, err error) error {
	if trace == nil {
		return err
	}
	return WrapDiagnostic(err, trace, DiagnosticSummary(trace.Snapshot().Stages))
}

func ptrString(value string) *string { return &value }

func (s *Service) ReportFrontendMedia(token string, code int, message string, retries int) AttemptDiagnostics {
	if _, ok := s.sessionDiagnostics(token); !ok {
		return AttemptDiagnostics{Stages: []DiagnosticStage{}}
	}
	if retries < 0 {
		retries = 0
	}
	if retries > 1 {
		retries = 1
	}
	outcome := OutcomeException
	if code == 0 {
		outcome = OutcomeOK
	} else if code == 1 {
		outcome = OutcomeHTTPError
	}
	if code != 0 {
		if session, ok := s.getEntry(token); ok && session.diagnostics != nil {
			session.diagnostics.SetFinalOutcome(outcome)
		}
	}
	return s.reportFrontendMedia(token, code, message, retries, outcome, code != 0)
}

// ReportFrontendMediaFailure closes an attempt even when the browser exposes
// no MediaError code (code zero is a valid missing-value signal).
func (s *Service) ReportFrontendMediaFailure(token string, code int, message string, retries int) AttemptDiagnostics {
	if _, ok := s.sessionDiagnostics(token); !ok {
		return AttemptDiagnostics{Stages: []DiagnosticStage{}}
	}
	outcome := OutcomeException
	if code == 1 {
		outcome = OutcomeHTTPError
	}
	if session, ok := s.getEntry(token); ok && session.diagnostics != nil {
		session.diagnostics.SetFinalOutcome(outcome)
	}
	return s.reportFrontendMedia(token, code, message, retries, outcome, true)
}

func (s *Service) ReportMediaReady(token string) AttemptDiagnostics {
	trace, ok := s.sessionDiagnostics(token)
	if !ok {
		return AttemptDiagnostics{Stages: []DiagnosticStage{}}
	}
	// A metadata-ready event is evidence for the frontend stage, but it is not
	// the final playback result: the stream can still fail after metadata loads.
	code := 0
	message := ""
	trace.Add(DiagnosticStage{Stage: "frontend_media", Outcome: OutcomeOK, FrontendMediaCode: &code, FrontendMessage: &message, Reason: stringPointer(frontendMediaReason(code)), Attempted: intPointer(0)})
	return trace.Snapshot()
}

func (s *Service) ReportMediaStreamFailure(token string, status int, contentType string, bytesRead int64, err error) AttemptDiagnostics {
	trace, ok := s.sessionDiagnostics(token)
	if !ok {
		return AttemptDiagnostics{Stages: []DiagnosticStage{}}
	}
	outcome, reason := OutcomeException, "stream_error"
	if errors.Is(err, context.DeadlineExceeded) {
		outcome, reason = OutcomeTimeout, "timeout"
	}
	trace.Add(DiagnosticStage{Stage: "session_media_fetch", Outcome: outcome, HTTPStatus: intPointer(status), ContentType: &contentType, Reason: stringPointer(reason), Attempted: intPointer(1), Refreshed: boolPointer(false), BytesRead: int64Pointer(bytesRead)})
	trace.Log(outcome)
	return trace.Snapshot()
}

func (s *Service) reportFrontendMedia(token string, code int, message string, retries int, outcome DiagnosticOutcome, logAttempt bool) AttemptDiagnostics {
	trace, ok := s.sessionDiagnostics(token)
	if !ok {
		return AttemptDiagnostics{Stages: []DiagnosticStage{}}
	}
	if retries < 0 {
		retries = 0
	}
	if retries > 1 {
		retries = 1
	}
	var codeValue *int
	if code > 0 {
		codeValue = intPointer(code)
	}
	trace.Add(DiagnosticStage{Stage: "frontend_media", Outcome: outcome, FrontendMediaCode: codeValue, FrontendMessage: &message, Reason: stringPointer(frontendMediaReason(code)), Attempted: intPointer(retries)})
	if logAttempt {
		trace.Log(outcome)
	}
	return trace.Snapshot()
}

func (s *Service) sessionDiagnostics(token string) (*DiagnosticTrace, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.sessions[token]
	if !ok || value.source.Platform != "douyin" || value.diagnostics == nil {
		return nil, false
	}
	return value.diagnostics, true
}

// ValidSource checks platform media boundaries for app-level composition.
func ValidSource(source Source) bool {
	if source.Platform == "" {
		source.Platform = "bilibili"
	}
	return allowedFor(source.Platform, source.URL)
}
