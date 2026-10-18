package playback

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestPrivateSourcesRangesAndCancellation(t *testing.T) {
	ctx := context.Background()
	requests := 0
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Range") != "bytes=2-5" || r.Header.Get("Referer") != "https://www.bilibili.com/" {
			t.Fatal("unsafe or incorrect media request")
		}
		return &http.Response{StatusCode: 206, Header: http.Header{"Content-Range": []string{"bytes 2-5/10"}}, Body: io.NopCloser(strings.NewReader("2345"))}, nil
	})}
	s := New(func(context.Context, string) (Source, error) {
		return Source{URL: "https://media.bilivideo.com/test.mp4?private=signed-fixture", Quality: 64}, nil
	}, client)
	if _, err := s.Open(ctx, "https://evil.example/movie"); !errors.Is(err, ErrInvalid) {
		t.Fatal("arbitrary input accepted")
	}
	job, err := s.Open(ctx, "BV17x411w7KC")
	if err != nil || len(job.ID) != 32 {
		t.Fatal("session not opened", err)
	}
	if _, _, err := s.Fetch(ctx, job.ID, "GET", "bytes=0-1,3-5"); !errors.Is(err, ErrInvalid) {
		t.Fatal("multipart accepted")
	}
	r, close, err := s.Fetch(ctx, job.ID, "GET", "bytes=2-5")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(r.Body)
	r.Body.Close()
	close()
	if string(data) != "2345" || requests != 1 {
		t.Fatal("range changed")
	}
	s.Release(job.ID)
	if _, _, err := s.Fetch(ctx, job.ID, "GET", ""); !errors.Is(err, ErrExpired) {
		t.Fatal("released stream remained accessible")
	}
	for _, raw := range []string{"http://media.bilivideo.com/test", "https://media.bilivideo.com.evil.example/a", "https://user:pass@media.bilivideo.com/a", "https://media.bilivideo.com:8000/a", "https://127.0.0.1/a"} {
		if allowed(raw) {
			t.Fatalf("unsafe source accepted: %s", raw)
		}
	}
	job, _ = s.Open(ctx, "BV17x411w7KC")
	s.mu.Lock()
	value := s.sessions[job.ID]
	value.expires = time.Now().Add(-time.Second)
	s.sessions[job.ID] = value
	s.mu.Unlock()
	if _, _, err := s.Fetch(ctx, job.ID, "GET", ""); !errors.Is(err, ErrExpired) {
		t.Fatal("expired stream remained accessible")
	}
}

func TestPlatformSpecificOpenMethodsRejectForeignIdentifiers(t *testing.T) {
	s := New(func(context.Context, string) (Source, error) {
		return Source{URL: "https://media.bilivideo.com/test.mp4"}, nil
	}, &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		return rangeResponse(200, "", "", "video/mp4"), nil
	})})
	if _, err := s.OpenBilibili(context.Background(), "7340916925458779430"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Bilibili route accepted a Douyin id: %v", err)
	}
	if _, err := s.OpenDouyin(context.Background(), "BV17x411w7KC"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Douyin route accepted a Bilibili id: %v", err)
	}
}

func TestDouyinSourceUsesDouyinReferer(t *testing.T) {
	s := New(func(context.Context, string) (Source, error) {
		return Source{URL: "https://v1.douyinvod.com/test.mp4", Platform: "douyin", Referer: "https://www.douyin.com/video/7340916925458779430"}, nil
	}, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Referer"); got != "https://www.douyin.com/video/7340916925458779430" {
			t.Fatalf("referer = %q", got)
		}
		return rangeResponse(200, "", "", "video/mp4"), nil
	})})
	job, err := s.OpenDouyin(context.Background(), "7340916925458779430")
	if err != nil {
		t.Fatal(err)
	}
	response, release, err := s.Fetch(context.Background(), job.ID, "GET", "")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	release()
}
func TestCanceledResolveDoesNotLeaveSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := New(func(context.Context, string) (Source, error) {
		cancel()
		return Source{URL: "https://media.bilivideo.com/test.mp4"}, nil
	}, http.DefaultClient)
	if _, err := s.Open(ctx, "BV17x411w7KC"); !errors.Is(err, context.Canceled) || len(s.sessions) != 0 {
		t.Fatal("aborted resolve leaked session")
	}
}
func TestUpstreamErrorsDoNotLeakSignedURL(t *testing.T) {
	s := New(func(context.Context, string) (Source, error) {
		return Source{URL: "https://media.bilivideo.com/test.mp4?private=must-stay-private"}, nil
	}, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) { return nil, errors.New(r.URL.String()) })})
	job, _ := s.Open(context.Background(), "BV17x411w7KC")
	_, _, err := s.Fetch(context.Background(), job.ID, "GET", "")
	if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "private") {
		t.Fatal("upstream error leaked")
	}
}

func TestBackupCDNFailsOverBeforeResponseBody(t *testing.T) {
	calls := []string{}
	s := New(func(context.Context, string) (Source, error) {
		return Source{URL: "https://first.bilivideo.com/a", Backups: []string{"https://evil.example/a", "https://second.bilivideo.com/a"}}, nil
	}, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.URL.Hostname())
		if r.URL.Hostname() == "first.bilivideo.com" {
			return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		if r.Header.Get("Range") != "bytes=4-7" {
			t.Fatal("range not preserved")
		}
		return &http.Response{StatusCode: 206, Header: http.Header{"Content-Range": {"bytes 4-7/10"}}, Body: io.NopCloser(strings.NewReader("4567"))}, nil
	})})
	job, err := s.Open(context.Background(), "BV17x411w7KC")
	if err != nil {
		t.Fatal(err)
	}
	response, release, err := s.Fetch(context.Background(), job.ID, "GET", "bytes=4-7")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	defer response.Body.Close()
	if len(calls) != 2 || calls[1] != "second.bilivideo.com" {
		t.Fatal(calls)
	}
}
