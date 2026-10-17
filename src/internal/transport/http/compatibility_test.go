package httptransport

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"tangentry/internal/playback"
)

type compatibilityTransport struct{}

func (compatibilityTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, ContentLength: 6, Body: io.NopCloser(strings.NewReader("source")), Header: make(http.Header)}, nil
}

func TestCompatibilityRoutesValidateLocatorAndServeSeekableTemporaryFile(t *testing.T) {
	s, err := playback.NewCompatibility(&http.Client{Transport: compatibilityTransport{}}, func(context.Context, string) (playback.Source, error) {
		return playback.Source{URL: "https://media.xhscdn.com/video.mp4"}, nil
	}, func(_ context.Context, _, output string) error {
		return os.WriteFile(output, []byte("0123456789 compatible fixture"), 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := New(nil, nil, nil, Options{Compatibility: s})
	send := func(method, path, body, byteRange string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "http://127.0.0.1/api/playback/compatibility"+path, strings.NewReader(body))
		r.Header.Set("Range", byteRange)
		h.ServeHTTP(w, r)
		return w
	}
	for _, body := range []string{`null`, `{}`, `{"url":"https://127.0.0.1/video"}`, `{"url":"https://xhslink.com/a/share"}`, `{"url":"https://www.xiaohongshu.com/explore/6411cf99000000001300b6d9","mediaUrl":"https://private.invalid"}`, `{"url":"https://www.xiaohongshu.com/explore/6411cf99000000001300b6d9"}{}`} {
		if w := send("POST", "", body, ""); w.Code != 400 {
			t.Fatalf("bad input accepted: %d", w.Code)
		}
	}
	w := send("POST", "", `{"url":"https://www.xiaohongshu.com/explore/6411cf99000000001300b6d9?xsec_token=private-fixture"}`, "")
	if w.Code != 202 || strings.Contains(w.Body.String(), "private-fixture") {
		t.Fatal("unsafe job creation", w.Code)
	}
	var job playback.CompatibilityJob
	if err := json.Unmarshal(w.Body.Bytes(), &job); err != nil || job.ID == "" {
		t.Fatal("missing job token", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for job.State != "ready" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		job, err = s.Get(job.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if job.State != "ready" {
		t.Fatal("conversion not completed")
	}
	media := send("GET", "/"+job.ID+"/media", "", "bytes=2-5")
	if media.Code != 206 || media.Body.String() != "2345" || media.Header().Get("Content-Type") != "video/mp4" || media.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("seekable media boundary failed", media.Code, media.Header())
	}
	if send("DELETE", "/"+job.ID, "", "").Code != 204 || send("GET", "/"+job.ID+"/media", "", "").Code != 410 {
		t.Fatal("released media remains available")
	}
}
