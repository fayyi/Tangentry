package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tangentry/internal/automaticsubtitles"
	"tangentry/internal/subtitles"
)

type automaticHTTPFixture struct {
	started   []struct{ id, language, capability string }
	appended  []automaticsubtitles.AudioChunk
	startErr  error
	appendErr error
}

func (f *automaticHTTPFixture) Start(_ context.Context, contentID, language, capability string, _, _ float64) (automaticsubtitles.Session, error) {
	f.started = append(f.started, struct{ id, language, capability string }{contentID, language, capability})
	if f.startErr != nil {
		return automaticsubtitles.Session{}, f.startErr
	}
	return automaticsubtitles.Session{ID: "session-1", ContentID: contentID, Language: language, Capability: capability, State: automaticsubtitles.Streaming}, nil
}
func (f *automaticHTTPFixture) Current(string) *automaticsubtitles.Session { return nil }
func (f *automaticHTTPFixture) Get(string) (automaticsubtitles.Session, error) {
	return automaticsubtitles.Session{}, automaticsubtitles.ErrNotFound
}
func (f *automaticHTTPFixture) Draft(string) (automaticsubtitles.Draft, error) {
	return automaticsubtitles.Draft{}, automaticsubtitles.ErrNotFound
}
func (f *automaticHTTPFixture) Playback(string, automaticsubtitles.Playback) (automaticsubtitles.Session, error) {
	return automaticsubtitles.Session{}, automaticsubtitles.ErrNotFound
}
func (f *automaticHTTPFixture) AppendAudio(_ string, chunk automaticsubtitles.AudioChunk) (automaticsubtitles.Session, error) {
	if len(chunk.Data) == 0 || len(chunk.Data)%2 != 0 || chunk.SampleRate != automaticsubtitles.PCMRate || chunk.Channels != automaticsubtitles.PCMChannels {
		return automaticsubtitles.Session{}, automaticsubtitles.ErrInvalid
	}
	f.appended = append(f.appended, chunk)
	if f.appendErr != nil {
		return automaticsubtitles.Session{}, f.appendErr
	}
	return automaticsubtitles.Session{ID: "session-1", State: automaticsubtitles.Streaming}, nil
}
func (f *automaticHTTPFixture) Finalize(context.Context, string) (subtitles.SavedTrack, error) {
	return subtitles.SavedTrack{}, automaticsubtitles.ErrBusy
}
func (f *automaticHTTPFixture) Pause(string) (automaticsubtitles.Session, error) {
	return automaticsubtitles.Session{}, automaticsubtitles.ErrNotFound
}
func (f *automaticHTTPFixture) Cancel(string) error { return automaticsubtitles.ErrNotFound }

func automaticRequest(handler http.Handler, method, path string, body io.Reader, metadata string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1"+path, body)
	if metadata != "" {
		r.Header.Set("X-Audio-Metadata", metadata)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestAutomaticAudioHTTPValidatesPCMAndMetadata(t *testing.T) {
	fixture := &automaticHTTPFixture{}
	handler := New(nil, nil, nil, Options{Automatic: fixture})
	validMetadata := `{"startSeconds":2.5,"sampleRate":16000,"channels":1,"final":true}`
	for _, tc := range []struct {
		name string
		body []byte
		meta string
		code int
	}{
		{name: "empty", body: nil, meta: validMetadata, code: http.StatusBadRequest},
		{name: "odd pcm", body: []byte{1}, meta: validMetadata, code: http.StatusBadRequest},
		{name: "wrong rate", body: []byte{1, 0}, meta: `{"startSeconds":0,"sampleRate":8000,"channels":1}`, code: http.StatusBadRequest},
		{name: "wrong channels", body: []byte{1, 0}, meta: `{"startSeconds":0,"sampleRate":16000,"channels":2}`, code: http.StatusBadRequest},
		{name: "bad metadata", body: []byte{1, 0}, meta: "{", code: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := len(fixture.appended)
			response := automaticRequest(handler, http.MethodPost, "/api/transcription/automatic/session-1/audio", bytes.NewReader(tc.body), tc.meta)
			if response.Code != tc.code {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if len(fixture.appended) != before {
				t.Fatalf("invalid request reached service: %#v", fixture.appended)
			}
		})
	}
	response := automaticRequest(handler, http.MethodPost, "/api/transcription/automatic/session-1/audio", bytes.NewReader([]byte{1, 0}), validMetadata)
	if response.Code != http.StatusAccepted || len(fixture.appended) != 1 || fixture.appended[0].StartSeconds != 2.5 || !fixture.appended[0].Final {
		t.Fatalf("valid audio rejected: %d %s %#v", response.Code, response.Body.String(), fixture.appended)
	}
}

func TestAutomaticAudioHTTPRejectsOversizedUnknownLengthBody(t *testing.T) {
	fixture := &automaticHTTPFixture{}
	handler := New(nil, nil, nil, Options{Automatic: fixture})
	body := io.LimitReader(strings.NewReader(strings.Repeat("x", (512<<10)+1)), (512<<10)+1)
	response := automaticRequest(handler, http.MethodPost, "/api/transcription/automatic/session-1/audio", body, `{"sampleRate":16000,"channels":1}`)
	if response.Code != http.StatusRequestEntityTooLarge {
		var output apiError
		_ = json.Unmarshal(response.Body.Bytes(), &output)
		t.Fatalf("oversized body status=%d code=%s body=%s", response.Code, output.Code, response.Body.String())
	}
	if len(fixture.appended) != 0 {
		t.Fatalf("oversized body reached service: %#v", fixture.appended)
	}
}

func TestAutomaticStartHTTPMapsBusyAndInvalidErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		code    int
		apiCode string
	}{
		{name: "busy", err: automaticsubtitles.ErrBusy, code: http.StatusConflict, apiCode: "automatic_subtitle_busy"},
		{name: "invalid", err: automaticsubtitles.ErrInvalid, code: http.StatusBadRequest, apiCode: "invalid_automatic_subtitle"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := &automaticHTTPFixture{startErr: tc.err}
			handler := New(nil, nil, nil, Options{Automatic: fixture})
			response := automaticRequest(handler, http.MethodPost, "/api/transcription/automatic", strings.NewReader(`{"contentId":"youtube:dQw4w9WgXcQ","language":"zh","capability":"streamable"}`), "")
			var output apiError
			if json.Unmarshal(response.Body.Bytes(), &output) != nil || response.Code != tc.code || output.Code != tc.apiCode {
				t.Fatalf("status=%d code=%s body=%s", response.Code, output.Code, response.Body.String())
			}
		})
	}
}
