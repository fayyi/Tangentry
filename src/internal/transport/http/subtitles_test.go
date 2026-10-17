package httptransport

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/subtitles"
)

func TestAcquiredVideoCaptionsAreAutomaticallySaved(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "captions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	saved := subtitles.NewSavedService(store)
	service := subtitles.NewService(captionProvider(func(_ context.Context, id string) (subtitles.Document, error) {
		return subtitles.Document{VideoID: id, Part: 1, Status: "available", Tracks: []subtitles.Track{{Language: "en", Label: "English", Cues: []subtitles.Cue{{Start: 0, End: 2, Text: "Automatically saved"}}}}}, nil
	}))
	defer service.Close()
	h := New(nil, nil, nil, Options{Subtitles: service, SavedSubtitles: saved})
	w := storageRequest(h, "GET", "/api/subtitles/bilibili?q=BV17x411w7KC", "", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	values, err := saved.List(context.Background(), "bilibili:BV17x411w7KC")
	if err != nil || len(values) != 1 || values[0].Track.Cues[0].Text != "Automatically saved" {
		t.Fatal(values, err)
	}
}

type captionProvider func(context.Context, string) (subtitles.Document, error)

func (f captionProvider) ReadSubtitles(ctx context.Context, id string) (subtitles.Document, error) {
	return f(ctx, id)
}

func TestSubtitleHTTPContractAndSafeErrors(t *testing.T) {
	calls := 0
	var upstream error
	h := New(nil, nil, nil, Options{Subtitles: subtitles.NewService(captionProvider(func(_ context.Context, id string) (subtitles.Document, error) {
		calls++
		if id != "BV17x411w7KC" {
			t.Errorf("bad video identity: %s", id)
		}
		return subtitles.Document{VideoID: id, Part: 1, Status: "login_required", Tracks: []subtitles.Track{}}, upstream
	}))})
	request := func(query string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/subtitles/bilibili?q="+query, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	if w := request("12345"); w.Code != 400 || calls != 0 {
		t.Fatal("invalid query invoked extractor")
	}
	w := request("BV17x411w7KC")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `"status":"login_required"`) || !strings.Contains(w.Body.String(), `"tracks":[]`) {
		t.Fatalf("bad login-required response: %d %s", w.Code, w.Body.String())
	}
	for _, test := range []struct {
		err    error
		status int
		code   string
	}{
		{subtitles.ErrTools, 503, "tools_unavailable"},
		{subtitles.ErrUnavailable, 502, "subtitles_unavailable"},
		{subtitles.ErrFormat, 502, "subtitles_invalid"},
		{subtitles.ErrLogin, 401, "subtitles_login_required"},
		{subtitles.ErrRestricted, 429, "subtitles_restricted"},
		{context.DeadlineExceeded, 504, "request_timeout"},
	} {
		upstream = errors.Join(test.err, errors.New("private-credential-that-must-not-be-forwarded"))
		w = request("BV17x411w7KC")
		if w.Code != test.status || !strings.Contains(w.Body.String(), test.code) || strings.Contains(w.Body.String(), "private-credential") {
			t.Fatalf("unexpected error response: %d %s", w.Code, w.Body.String())
		}
	}
	noTools := New(nil, nil, nil)
	w = httptest.NewRecorder()
	noTools.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/subtitles/bilibili?q=BV17x411w7KC", nil))
	if w.Code != 503 {
		t.Fatal("missing reader reported ready")
	}
}

type youtubeCaptionProvider struct {
	captionProvider
	read func(context.Context, string, string) (subtitles.Document, error)
}

func (p youtubeCaptionProvider) ReadYouTubeSubtitles(ctx context.Context, id, language string) (subtitles.Document, error) {
	return p.read(ctx, id, language)
}

func TestYouTubeCaptionHTTPSelectionAndFailureContract(t *testing.T) {
	calls := 0
	var upstream error
	service := subtitles.NewService(youtubeCaptionProvider{read: func(_ context.Context, id, language string) (subtitles.Document, error) {
		calls++
		if id != "jNQXAC9IVRw" || language != "en" {
			t.Errorf("unexpected selector %s %s", id, language)
		}
		return subtitles.Document{VideoID: id, Part: 1, Status: "available", Languages: []subtitles.Language{{Language: "en", Label: "英语"}}, Tracks: []subtitles.Track{{Language: "en", Label: "英语", Cues: []subtitles.Cue{{Start: 0, End: 1, Text: "caption"}}}}}, upstream
	}})
	defer service.Close()
	h := New(nil, nil, nil, Options{Subtitles: service})
	endpoint := "/api/subtitles/youtube?q=https%3A%2F%2Fyoutu.be%2FjNQXAC9IVRw&language=en"
	w := storageRequest(h, "GET", endpoint, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"language":"en"`) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("YouTube HTTP contract failed")
	}
	w = storageRequest(h, "GET", "/api/subtitles/youtube?q=invalid&language=en", "", "")
	if w.Code != 400 || calls != 1 {
		t.Fatal("invalid source reached extractor")
	}
	upstream = errors.Join(subtitles.ErrUnavailable, errors.New("private-caption-url-and-cookie"))
	w = storageRequest(h, "GET", endpoint, "", "")
	if w.Code != 502 || strings.Contains(w.Body.String(), "private-caption") {
		t.Fatal("upstream output leaked or empty success")
	}
}
