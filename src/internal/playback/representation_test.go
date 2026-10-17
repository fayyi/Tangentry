package playback

import (
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync/atomic"
	"testing"
)

func TestMediaClientDoesNotInheritAnAPICookieJar(t *testing.T) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := url.Parse("https://media.bilivideo.com/video")
	jar.SetCookies(target, []*http.Cookie{{Name: "fixture_private", Value: "must-not-be-forwarded"}})
	s := New(func(context.Context, string) (Source, error) { return Source{URL: target.String()}, nil }, &http.Client{Jar: jar, Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Cookie") != "" {
			t.Fatal("API cookie jar leaked to media")
		}
		return rangeResponse(206, "bytes 2-5/10", "4", "video/mp4"), nil
	})})
	job, _ := s.Open(context.Background(), "BV17x411w7KC")
	response, release, err := s.Fetch(context.Background(), job.ID, "GET", "bytes=2-5")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	release()
	if len(jar.Cookies(target)) != 1 {
		t.Fatal("shared API jar changed")
	}
}

func TestSignedRefreshCannotSilentlyChangeRepresentation(t *testing.T) {
	var resolves atomic.Int32
	s := New(func(context.Context, string) (Source, error) {
		quality := 64
		if resolves.Add(1) > 1 {
			quality = 80
		}
		return Source{URL: "https://media.bilivideo.com/video", Quality: quality, Duration: 123}, nil
	}, &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { return rangeResponse(403, "", "", "text/html"), nil })})
	job, _ := s.Open(context.Background(), "BV17x411w7KC")
	if _, _, err := s.Fetch(context.Background(), job.ID, "GET", "bytes=2-5"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("quality switched under a byte seek", err)
	}
}

func TestBackupWithDifferentFileLengthDoesNotContinuePreviousByteOffsets(t *testing.T) {
	var requests atomic.Int32
	s := New(func(context.Context, string) (Source, error) {
		return Source{URL: "https://first.bilivideo.com/video", Backups: []string{"https://second.bilivideo.com/video"}}, nil
	}, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if requests.Add(1) == 1 {
			return rangeResponse(206, "bytes 2-5/10", "4", "video/mp4"), nil
		}
		if r.URL.Hostname() == "first.bilivideo.com" {
			return rangeResponse(503, "", "", ""), nil
		}
		return rangeResponse(206, "bytes 2-5/11", "4", "video/mp4"), nil
	})})
	job, _ := s.Open(context.Background(), "BV17x411w7KC")
	response, release, err := s.Fetch(context.Background(), job.ID, "GET", "bytes=2-5")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	release()
	if _, _, err = s.Fetch(context.Background(), job.ID, "GET", "bytes=2-5"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("incompatible resource accepted", err)
	}
}

func TestPersistentForbiddenRefreshIsBoundedAcrossRepeatedRequests(t *testing.T) {
	var resolves atomic.Int32
	s := New(func(context.Context, string) (Source, error) {
		resolves.Add(1)
		return Source{URL: "https://media.bilivideo.com/video"}, nil
	}, &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { return rangeResponse(403, "", "", "text/html"), nil })})
	job, _ := s.Open(context.Background(), "BV17x411w7KC")
	for range 3 {
		if _, _, err := s.Fetch(context.Background(), job.ID, "GET", "bytes=2-5"); !errors.Is(err, ErrUnavailable) {
			t.Fatal(err)
		}
	}
	if resolves.Load() != 2 {
		t.Fatal("persistent rejection hammered metadata", resolves.Load())
	}
}
