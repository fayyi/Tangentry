// Package automaticsubtitles coordinates local-first caption sessions. It is
// intentionally separate from the explicit transcription service so drafts
// can be displayed and resumed without becoming formal saved subtitles.
package automaticsubtitles

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"tangentry/internal/automaticsubtitles/worker"
	"tangentry/internal/content"
	"tangentry/internal/subtitles"
	"tangentry/internal/transcription"
)

var (
	ErrInvalid  = errors.New("invalid automatic subtitle session")
	ErrNotFound = errors.New("automatic subtitle session not found")
	ErrBusy     = errors.New("automatic subtitle session busy")
)

const (
	PCMRate       = 16000
	PCMChannels   = 1
	maxPCMBytes   = PCMRate * PCMChannels * 2 * 15
	maxPCMSeconds = 15
)

type State string

const (
	Preparing  State = "preparing"
	Streaming  State = "streaming"
	Finalizing State = "finalizing"
	Paused     State = "paused"
	Completed  State = "completed"
	Failed     State = "failed"
	Canceled   State = "canceled"
)

type Session struct {
	ID               string  `json:"id"`
	ContentID        string  `json:"contentId"`
	Language         string  `json:"language"`
	Capability       string  `json:"capability"`
	State            State   `json:"state"`
	ProcessedSeconds float64 `json:"processedSeconds"`
	FurthestPosition float64 `json:"furthestPosition"`
	DurationSeconds  float64 `json:"durationSeconds"`
	JobID            string  `json:"jobId,omitempty"`
	ErrorCode        string  `json:"errorCode,omitempty"`
	ErrorMessage     string  `json:"errorMessage,omitempty"`
	UpdatedAt        int64   `json:"updatedAt"`
}

type Playback struct {
	PositionSeconds float64 `json:"positionSeconds"`
	DurationSeconds float64 `json:"durationSeconds"`
	Playing         bool    `json:"playing"`
	Seeked          bool    `json:"seeked"`
	Ended           bool    `json:"ended"`
}

// AudioChunk is a local-only PCM16 mono frame from a playable media element.
// The server never forwards this data to a cloud provider.
type AudioChunk struct {
	Data         []byte
	StartSeconds float64
	SampleRate   int
	Channels     int
	Final        bool
}

type Draft struct {
	SessionID string          `json:"sessionId"`
	Track     subtitles.Track `json:"track"`
	UpdatedAt int64           `json:"updatedAt"`
}

// StoredSession is the persistence shape for an automatic session. Drafts are
// kept here until finalization and are intentionally separate from saved
// subtitles/search indexing.
type StoredSession struct {
	Session  Session
	Draft    subtitles.Track
	Revision int64
}

type SessionStore interface {
	ListAutomaticSessions(context.Context, string) ([]StoredSession, error)
	GetAutomaticSession(context.Context, string) (StoredSession, error)
	SaveAutomaticSession(context.Context, StoredSession) error
}

type transcriptionPort interface {
	StartAutomatic(context.Context, string, string) (transcription.Job, error)
	Get(string) (transcription.Job, error)
	Result(string) (subtitles.Track, bool)
	Cancel(string) error
}

// StreamingRecognizer is intentionally small so the automatic session can be
// tested without a model or process. Implementations must remain local-only.
type StreamingRecognizer interface {
	RecognizePCM(context.Context, string, []byte, float64) (subtitles.Track, error)
}

// StreamingCapability is optional so existing test recognizers and future
// worker implementations can continue to satisfy StreamingRecognizer. When
// present, the coordinator uses it before accepting a streamable session;
// unsupported local backends then take the existing batch-only path.
type StreamingCapability interface {
	StreamingReady() bool
}

type savedPort interface {
	List(context.Context, string) ([]subtitles.SavedTrack, error)
	Save(context.Context, string, subtitles.Track, int64) (subtitles.SavedTrack, error)
}

type sessionRecord struct {
	Session
	Draft            subtitles.Track
	Revision         int64
	merger           *worker.Merger
	stream           *audioStream
	finalizeInFlight bool
}

type audioStream struct {
	ctx           context.Context
	cancel        context.CancelFunc
	queue         chan AudioChunk
	done          chan struct{}
	seq           int64
	closed        bool
	discardEvents bool
}

type Service struct {
	mu            sync.Mutex
	transcription transcriptionPort
	saved         savedPort
	store         SessionStore
	sessions      map[string]*sessionRecord
	byContent     map[string]string
	closed        bool
	recognizer    StreamingRecognizer
	streams       map[string]*audioStream
}

func New(transcriptionService transcriptionPort, saved savedPort, stores ...SessionStore) *Service {
	var store SessionStore
	if len(stores) > 0 {
		store = stores[0]
	}
	return &Service{transcription: transcriptionService, saved: saved, store: store, sessions: map[string]*sessionRecord{}, byContent: map[string]string{}, streams: map[string]*audioStream{}}
}

func (s *Service) SetStreamingRecognizer(recognizer StreamingRecognizer) {
	s.mu.Lock()
	s.recognizer = recognizer
	s.mu.Unlock()
}

func (s *Service) Start(ctx context.Context, contentID, language, capability string, start, duration float64) (Session, error) {
	if !content.SpeechID(contentID) || strings.TrimSpace(language) == "" || capability == "" || capability != "batch-only" && capability != "streamable" {
		return Session{}, ErrInvalid
	}
	tracks, err := s.saved.List(ctx, contentID)
	if err != nil {
		return Session{}, err
	}
	for _, value := range tracks {
		if len(value.Track.Cues) > 0 {
			return Session{}, ErrBusy
		}
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return Session{}, ErrInvalid
	}
	if capability == "streamable" && !streamingReady(s.recognizer) {
		capability = "batch-only"
	}
	var record *sessionRecord
	var previousStream *audioStream
	if existing := s.byContent[contentID]; existing != "" {
		if value := s.sessions[existing]; value != nil {
			switch value.State {
			case Completed, Failed, Canceled:
				// A later playback may start a fresh session after a terminal one.
			case Paused:
				// A persisted paused session has no process-local job after a
				// restart. Reuse its draft and revision, then attach a new job.
				if value.JobID == "" {
					record = value
					previousStream = value.stream
				}
				if record == nil {
					result := value.Session
					s.mu.Unlock()
					return result, nil
				}
			default:
				result := value.Session
				s.mu.Unlock()
				return result, nil
			}
		}
	}
	if record == nil {
		id, err := newID()
		if err != nil {
			s.mu.Unlock()
			return Session{}, err
		}
		now := time.Now().Unix()
		record = &sessionRecord{Session: Session{ID: id, ContentID: contentID, Language: language, Capability: capability, State: Preparing, FurthestPosition: start, DurationSeconds: duration, UpdatedAt: now}, merger: worker.NewMerger()}
		s.sessions[id], s.byContent[contentID] = record, id
		s.persistLocked(record)
	}
	s.mu.Unlock()
	if previousStream != nil {
		select {
		case <-previousStream.done:
		case <-ctx.Done():
			return Session{}, ctx.Err()
		}
	}
	if record != nil {
		s.mu.Lock()
		// The old stream has fully exited before this point, so its final event
		// cannot overwrite the new playback generation.
		record.Capability = capability
		record.State = Preparing
		record.ErrorCode, record.ErrorMessage = "", ""
		record.UpdatedAt = time.Now().Unix()
		s.persistLocked(record)
		s.mu.Unlock()
	}
	if capability == "streamable" {
		s.mu.Lock()
		streamCtx, cancel := context.WithCancel(context.Background())
		// Keep enough decoded audio buffered to absorb a slower recognition
		// pass without making the browser drop a frame while the model is warm.
		stream := &audioStream{ctx: streamCtx, cancel: cancel, queue: make(chan AudioChunk, 16), done: make(chan struct{})}
		record.stream = stream
		s.streams[record.ID] = stream
		record.State = Streaming
		record.JobID = ""
		record.UpdatedAt = time.Now().Unix()
		s.persistLocked(record)
		result := record.Session
		s.mu.Unlock()
		go s.runAudioStream(record.ID, stream)
		return result, nil
	}
	job, err := s.transcription.StartAutomatic(ctx, contentID, language)
	if err != nil {
		s.mu.Lock()
		record.State, record.ErrorCode, record.ErrorMessage = Paused, "transcription_start", err.Error()
		record.UpdatedAt = time.Now().Unix()
		s.persistLocked(record)
		s.mu.Unlock()
		return record.Session, err
	}
	s.mu.Lock()
	record.JobID, record.State, record.UpdatedAt = job.ID, Streaming, time.Now().Unix()
	s.persistLocked(record)
	result := record.Session
	s.mu.Unlock()
	return result, nil
}

func streamingReady(recognizer StreamingRecognizer) bool {
	if recognizer == nil {
		return false
	}
	if capability, ok := recognizer.(StreamingCapability); ok {
		return capability.StreamingReady()
	}
	return true
}

func (s *Service) AppendAudio(id string, chunk AudioChunk) (Session, error) {
	if len(chunk.Data) == 0 || len(chunk.Data) > maxPCMBytes || len(chunk.Data)%2 != 0 || chunk.SampleRate != PCMRate || chunk.Channels != PCMChannels || math.IsNaN(chunk.StartSeconds) || math.IsInf(chunk.StartSeconds, 0) || chunk.StartSeconds < 0 {
		return Session{}, ErrInvalid
	}
	s.mu.Lock()
	value := s.sessions[id]
	if value == nil {
		s.mu.Unlock()
		return Session{}, ErrNotFound
	}
	stream := value.stream
	if stream == nil || value.Capability != "streamable" || stream.closed || value.State == Canceled || value.State == Failed {
		s.mu.Unlock()
		return Session{}, ErrBusy
	}
	copyChunk := AudioChunk{Data: append([]byte(nil), chunk.Data...), StartSeconds: chunk.StartSeconds, SampleRate: chunk.SampleRate, Channels: chunk.Channels, Final: chunk.Final}
	queue, streamCtx := stream.queue, stream.ctx
	result := value.Session
	// Keep the service lock while enqueueing and closing. This makes the final
	// frame and the queue close atomic with respect to pause/cancel transitions.
	select {
	case queue <- copyChunk:
		if chunk.Final {
			s.closeStreamLocked(stream)
		}
		s.mu.Unlock()
		return result, nil
	case <-streamCtx.Done():
		s.mu.Unlock()
		return Session{}, ErrBusy
	default:
		s.mu.Unlock()
		return Session{}, ErrBusy
	}
}

func (s *Service) runAudioStream(id string, stream *audioStream) {
	defer close(stream.done)
	defer func() {
		s.mu.Lock()
		if current := s.sessions[id]; current != nil && current.stream == stream {
			delete(s.streams, id)
			current.stream = nil
		}
		s.mu.Unlock()
	}()
	for {
		var chunk AudioChunk
		var ok bool
		select {
		case <-stream.ctx.Done():
			return
		case chunk, ok = <-stream.queue:
			if !ok {
				return
			}
		}
		track, err := s.recognizePCM(stream.ctx, id, chunk)
		if err != nil {
			_, _ = s.applyStreamEvent(id, stream, worker.Event{Type: "error", Code: "stream_recognition", Message: err.Error()})
			return
		}
		for _, cue := range track.Cues {
			stream.seq++
			if _, err := s.applyStreamEvent(id, stream, worker.Event{Type: "final", Sequence: stream.seq, Start: cue.Start, End: cue.End, Text: cue.Text}); err != nil {
				return
			}
		}
		if chunk.Final {
			_, _ = s.applyStreamEvent(id, stream, worker.Event{Type: "stopped"})
			return
		}
	}
}

func (s *Service) recognizePCM(ctx context.Context, id string, chunk AudioChunk) (subtitles.Track, error) {
	s.mu.Lock()
	recognizer := s.recognizer
	value := s.sessions[id]
	language := ""
	if value != nil {
		language = value.Language
	}
	s.mu.Unlock()
	if recognizer == nil || language == "" {
		return subtitles.Track{}, ErrInvalid
	}
	return recognizer.RecognizePCM(ctx, language, chunk.Data, chunk.StartSeconds)
}

func (s *Service) Get(id string) (Session, error) {
	s.load(id, "")
	s.mu.Lock()
	defer s.mu.Unlock()
	value := s.sessions[id]
	if value == nil {
		return Session{}, ErrNotFound
	}
	s.refreshLocked(value)
	return value.Session, nil
}

func (s *Service) Current(contentID string) *Session {
	s.load("", contentID)
	s.mu.Lock()
	defer s.mu.Unlock()
	value := s.sessions[s.byContent[contentID]]
	if value == nil {
		return nil
	}
	s.refreshLocked(value)
	result := value.Session
	return &result
}

func (s *Service) Draft(id string) (Draft, error) {
	s.load(id, "")
	s.mu.Lock()
	defer s.mu.Unlock()
	value := s.sessions[id]
	if value == nil {
		return Draft{}, ErrNotFound
	}
	s.refreshLocked(value)
	return Draft{SessionID: id, Track: value.Draft, UpdatedAt: value.UpdatedAt}, nil
}

func (s *Service) Playback(id string, playback Playback) (Session, error) {
	if math.IsNaN(playback.PositionSeconds) || math.IsInf(playback.PositionSeconds, 0) || math.IsNaN(playback.DurationSeconds) || math.IsInf(playback.DurationSeconds, 0) || playback.PositionSeconds < 0 || playback.DurationSeconds < 0 {
		return Session{}, ErrInvalid
	}
	s.mu.Lock()
	value := s.sessions[id]
	if value == nil {
		s.mu.Unlock()
		return Session{}, ErrNotFound
	}
	if playback.PositionSeconds > value.FurthestPosition {
		value.FurthestPosition = playback.PositionSeconds
	}
	if playback.DurationSeconds > 0 {
		value.DurationSeconds = playback.DurationSeconds
	}
	if playback.PositionSeconds > value.ProcessedSeconds {
		value.ProcessedSeconds = playback.PositionSeconds
	}
	if playback.Seeked && value.stream != nil {
		// Audio captured before a seek belongs to the old media timeline. Drop
		// its queued frames and let the next playback heartbeat start a fresh
		// stream at the requested position.
		value.stream.discardEvents = true
		s.closeStreamLocked(value.stream)
		if playback.Playing {
			value.State = Paused
		}
	}
	if playback.Playing && value.State == Paused && value.JobID != "" {
		value.State = Streaming
	}
	if !playback.Playing && !playback.Ended && (value.State == Streaming || value.State == Preparing) {
		value.State = Paused
		if value.stream != nil {
			// A normal pause still allows audio already uploaded by the browser
			// to drain. Seeking and cancellation explicitly mark their old
			// timeline as discardable; pausing should preserve the final short
			// frame so the last spoken sentence is not lost.
			s.closeStreamLocked(value.stream)
		}
	}
	if playback.Ended && !playback.Playing && value.State != Completed && value.State != Failed {
		value.State = Finalizing
		if value.stream != nil {
			s.closeStreamLocked(value.stream)
		}
	}
	value.UpdatedAt = time.Now().Unix()
	s.refreshLocked(value)
	s.persistLocked(value)
	result := value.Session
	s.mu.Unlock()
	if playback.Ended && !playback.Playing {
		// Finalize waits for a stream that is still draining, so the last audio
		// frame cannot be omitted when the ended event races the final upload.
		_, _ = s.Finalize(context.Background(), id)
	}
	return result, nil
}

// ApplyWorkerEvent accepts a desktop recognizer event and folds it into the
// durable draft. The event path is deliberately separate from the batch
// transcription job so a future PCM bridge can stream partial cues without
// changing the HTTP/session contract.
func (s *Service) ApplyWorkerEvent(id string, event worker.Event) (Draft, error) {
	s.load(id, "")
	return s.applyWorkerEvent(id, nil, event)
}

// applyStreamEvent folds an event only while its originating stream is still
// current and allowed to publish. A paused/canceled stream may finish draining
// queued audio, but its late events must not revive the session or finalize a
// newer playback generation.
func (s *Service) applyStreamEvent(id string, stream *audioStream, event worker.Event) (Draft, error) {
	return s.applyWorkerEvent(id, stream, event)
}

func (s *Service) applyWorkerEvent(id string, stream *audioStream, event worker.Event) (Draft, error) {
	s.mu.Lock()
	value := s.sessions[id]
	if value == nil {
		s.mu.Unlock()
		return Draft{}, ErrNotFound
	}
	if stream != nil && (value.stream != stream || stream.discardEvents) {
		result := Draft{SessionID: id, Track: value.Draft, UpdatedAt: value.UpdatedAt}
		s.mu.Unlock()
		return result, nil
	}
	if event.SessionID != "" && event.SessionID != id {
		s.mu.Unlock()
		return Draft{}, fmt.Errorf("%w: worker session mismatch", ErrInvalid)
	}
	if value.merger == nil {
		value.merger = worker.NewMergerFromTrack(value.Draft)
	}
	switch event.Type {
	case "hello", "ready":
		if value.State == Preparing || value.State == Paused {
			value.State = Streaming
		}
	case "partial", "final":
		if err := value.merger.Apply(event); err != nil {
			s.mu.Unlock()
			return Draft{}, fmt.Errorf("%w: %v", ErrInvalid, err)
		}
		value.Draft = value.merger.Track(value.Language, "自动字幕")
		// A stream may finish draining after a normal pause or an ended
		// playback. Keep that externally visible state while still publishing
		// the queued cue into the durable draft.
		if value.State != Paused && value.State != Finalizing {
			value.State = Streaming
		}
	case "error":
		value.State = Failed
		value.ErrorCode = event.Code
		value.ErrorMessage = event.Message
	case "stopped":
		if value.State == Finalizing || value.State == Paused {
			// Playback.Ended already put the session into finalizing; leave it
			// there until Finalize saves the drained draft. A normal pause also
			// remains paused after its already-uploaded audio drains.
		} else if len(value.Draft.Cues) > 0 {
			value.State = Finalizing
		} else {
			value.State = Paused
		}
	default:
		s.mu.Unlock()
		return Draft{}, fmt.Errorf("%w: unsupported worker event %q", ErrInvalid, event.Type)
	}
	value.UpdatedAt = time.Now().Unix()
	s.persistLocked(value)
	result := Draft{SessionID: id, Track: value.Draft, UpdatedAt: value.UpdatedAt}
	s.mu.Unlock()
	return result, nil
}

func (s *Service) Finalize(ctx context.Context, id string) (subtitles.SavedTrack, error) {
	for {
		s.mu.Lock()
		value := s.sessions[id]
		if value == nil {
			s.mu.Unlock()
			return subtitles.SavedTrack{}, ErrNotFound
		}
		stream := value.stream
		if stream != nil {
			s.closeStreamLocked(stream)
			s.mu.Unlock()
			select {
			case <-stream.done:
				continue
			case <-ctx.Done():
				return subtitles.SavedTrack{}, ctx.Err()
			}
		}
		s.refreshLocked(value)
		if len(value.Draft.Cues) == 0 || value.finalizeInFlight {
			s.mu.Unlock()
			return subtitles.SavedTrack{}, ErrBusy
		}
		track, revision, contentID := value.Draft, value.Revision, value.ContentID
		value.finalizeInFlight = true
		value.State, value.UpdatedAt = Finalizing, time.Now().Unix()
		s.persistLocked(value)
		s.mu.Unlock()

		saved, err := s.saved.Save(ctx, contentID, track, revision)
		s.mu.Lock()
		value.finalizeInFlight = false
		if err != nil {
			value.State, value.ErrorCode, value.ErrorMessage = Failed, "subtitle_save", err.Error()
			value.UpdatedAt = time.Now().Unix()
			s.persistLocked(value)
			s.mu.Unlock()
			return subtitles.SavedTrack{}, err
		}
		value.State, value.UpdatedAt = Completed, time.Now().Unix()
		s.persistLocked(value)
		s.mu.Unlock()
		return saved, nil
	}
}

func (s *Service) Pause(id string) (Session, error) { return s.transition(id, Paused) }
func (s *Service) Cancel(id string) error {
	s.load(id, "")
	s.mu.Lock()
	value := s.sessions[id]
	if value == nil {
		s.mu.Unlock()
		return ErrNotFound
	}
	jobID := value.JobID
	if value.stream != nil {
		value.stream.discardEvents = true
		s.closeStreamLocked(value.stream)
		value.stream.cancel()
	}
	value.State, value.UpdatedAt = Canceled, time.Now().Unix()
	s.persistLocked(value)
	s.mu.Unlock()
	if jobID != "" {
		_ = s.transcription.Cancel(jobID)
	}
	return nil
}

func (s *Service) transition(id string, state State) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value := s.sessions[id]
	if value == nil {
		return Session{}, ErrNotFound
	}
	value.State, value.UpdatedAt = state, time.Now().Unix()
	if state == Paused && value.stream != nil {
		value.stream.discardEvents = true
		s.closeStreamLocked(value.stream)
	}
	s.persistLocked(value)
	return value.Session, nil
}

func (s *Service) refreshLocked(value *sessionRecord) {
	if value.JobID != "" {
		job, err := s.transcription.Get(value.JobID)
		if err == nil {
			switch job.State {
			case "completed":
				if track, ok := s.transcription.Result(value.JobID); ok {
					value.Draft = track
					value.merger = worker.NewMergerFromTrack(track)
					value.State = Finalizing
					s.persistLocked(value)
				}
			case "failed":
				value.State, value.ErrorCode, value.ErrorMessage = Failed, job.ErrorCode, job.Error
				s.persistLocked(value)
			case "canceled":
				value.State = Canceled
				s.persistLocked(value)
			}
		}
	}
	value.UpdatedAt = time.Now().Unix()
}

func (s *Service) persistLocked(value *sessionRecord) {
	if s.store == nil || value == nil {
		return
	}
	_ = s.store.SaveAutomaticSession(context.Background(), StoredSession{Session: value.Session, Draft: value.Draft, Revision: value.Revision})
}

// closeStreamLocked prevents new audio and closes the queue after the caller
// has acquired the service lock. Existing queued chunks are still drained by
// runAudioStream; cancellation is reserved for an explicit cancel/close.
func (s *Service) closeStreamLocked(stream *audioStream) {
	if stream == nil || stream.closed {
		return
	}
	stream.closed = true
	close(stream.queue)
}

// load hydrates a session once from SQLite. Running jobs remain owned by the
// current process; persisted drafts remain available after a restart.
func (s *Service) load(id, contentID string) {
	if s.store == nil {
		return
	}
	s.mu.Lock()
	if (id != "" && s.sessions[id] != nil) || (contentID != "" && s.byContent[contentID] != "") {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	var values []StoredSession
	var err error
	if id != "" {
		value, getErr := s.store.GetAutomaticSession(context.Background(), id)
		err = getErr
		if err == nil {
			values = []StoredSession{value}
		}
	} else {
		values, err = s.store.ListAutomaticSessions(context.Background(), contentID)
	}
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, stored := range values {
		if s.sessions[stored.Session.ID] != nil {
			continue
		}
		record := &sessionRecord{Session: stored.Session, Draft: stored.Draft, Revision: stored.Revision, merger: worker.NewMergerFromTrack(stored.Draft)}
		// A process-local job cannot be resumed by its old ID after restart.
		if record.State == Streaming || record.State == Preparing {
			record.State = Paused
			record.JobID = ""
		}
		s.sessions[record.ID] = record
		s.byContent[record.ContentID] = record.ID
	}
}

func newID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	streams := make([]*audioStream, 0, len(s.streams))
	for _, stream := range s.streams {
		streams = append(streams, stream)
		stream.discardEvents = true
		s.closeStreamLocked(stream)
		stream.cancel()
	}
	s.mu.Unlock()
	for _, stream := range streams {
		<-stream.done
	}
}
