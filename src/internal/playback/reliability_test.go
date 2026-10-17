package playback

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func rangeResponse(status int, contentRange, length, mediaType string) *http.Response {
	h := http.Header{}
	if contentRange != "" {
		h.Set("Content-Range", contentRange)
	}
	if length != "" {
		h.Set("Content-Length", length)
	}
	if mediaType != "" {
		h.Set("Content-Type", mediaType)
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader("bytes"))}
}

func TestMediaRangeValidationDoesNotPassWrongBytesOrLoginPages(t *testing.T) {
	for _, row := range []struct {
		request          string
		status           int
		cr, length, mime string
		valid            bool
	}{
		{"", 200, "", "10", "video/mp4", true},
		{"", 200, "", "10", "text/html", false},
		{"bytes=2-5", 206, "bytes 2-5/10", "4", "video/mp4", true},
		{"bytes=2-5", 206, "bytes 0-3/10", "4", "video/mp4", false},
		{"bytes=2-5", 206, "bytes 2-5/10", "10", "video/mp4", false},
		{"bytes=2-5", 206, "", "4", "video/mp4", false},
		{"bytes=2-5", 200, "", "10", "video/mp4", false},
		{"bytes=2-5", 206, "bytes 2-5/10", "4", "application/json", false},
		{"bytes=2-", 206, "bytes 2-9/10", "8", "application/octet-stream", true},
		{"bytes=2-99", 206, "bytes 2-9/10", "8", "video/mp4", true},
		{"bytes=-3", 206, "bytes 7-9/10", "3", "video/mp4", true},
		{"bytes=-99", 206, "bytes 0-9/10", "10", "video/mp4", true},
		{"bytes=-3", 206, "bytes 0-2/10", "3", "video/mp4", false},
		{"bytes=2-5", 206, "bytes 2-5/*", "4", "video/mp4", false},
		{"bytes=2-5", 206, "bytes 2-5/5", "4", "video/mp4", false},
		{"bytes=99-", 416, "bytes */10", "", "", true},
		{"bytes=99-", 416, "bytes */10", "", "text/plain", true},
		{"bytes=2-5", 416, "bytes */10", "", "", false},
		{"", 206, "bytes 0-9/10", "10", "video/mp4", false},
	} {
		response := rangeResponse(row.status, row.cr, row.length, row.mime)
		if got := validMediaResponse(response, row.request); got != row.valid {
			t.Fatalf("%+v: got %v", row, got)
		}
		response.Body.Close()
	}
	for _, raw := range []string{"bytes=5-2", "bytes=-0", "bytes=999999999999999999999999-", "bytes=0-1,3-4", "items=0-1"} {
		if validRangeInput(raw) {
			t.Fatal("invalid input accepted", raw)
		}
	}
}

func TestBadResponsesUseAllBoundedBackupsAndRememberWorkingSource(t *testing.T) {
	var calls []string
	s := New(func(context.Context, string) (Source, error) {
		return Source{URL: "https://first.bilivideo.com/a", Backups: []string{"https://evil.example/a", "https://second.bilivideo.com/a", "https://second.bilivideo.com/a", "https://third.bilivideo.com/a"}}, nil
	}, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.URL.Hostname())
		switch r.URL.Hostname() {
		case "first.bilivideo.com":
			return rangeResponse(200, "", "10", "text/html"), nil
		case "second.bilivideo.com":
			return rangeResponse(206, "bytes 0-3/10", "4", "video/mp4"), nil
		}
		return rangeResponse(206, "bytes 2-5/10", "4", "video/mp4"), nil
	})})
	job, err := s.Open(context.Background(), "BV17x411w7KC")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		response, release, err := s.Fetch(context.Background(), job.ID, "GET", "bytes=2-5")
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		release()
	}
	if !reflect.DeepEqual(calls, []string{"first.bilivideo.com", "second.bilivideo.com", "third.bilivideo.com", "third.bilivideo.com"}) {
		t.Fatal(calls)
	}
}

func TestExpiredAddressRefreshesOnceForConcurrentSeeksAndReleaseCannotResurrectSession(t *testing.T) {
	var resolves atomic.Int32
	entered := make(chan struct{}, 1)
	gate := make(chan struct{})
	s := New(func(ctx context.Context, _ string) (Source, error) {
		count := resolves.Add(1)
		if count > 1 {
			entered <- struct{}{}
			select {
			case <-gate:
			case <-ctx.Done():
				return Source{}, ctx.Err()
			}
		}
		return Source{URL: fmt.Sprintf("https://media.bilivideo.com/a?revision=%d&deadline=%d", count, time.Now().Add(time.Hour).Unix())}, nil
	}, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return rangeResponse(206, "bytes 2-5/10", "4", "video/mp4"), nil
	})})
	job, _ := s.Open(context.Background(), "BV17x411w7KC")
	s.mu.Lock()
	value := s.sessions[job.ID]
	value.sourceExpires = time.Now().Add(-time.Second)
	s.sessions[job.ID] = value
	s.mu.Unlock()
	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			response, release, err := s.Fetch(context.Background(), job.ID, "GET", "bytes=2-5")
			if err == nil {
				response.Body.Close()
				release()
			}
			errs <- err
		}()
	}
	<-entered
	close(gate)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if resolves.Load() != 2 {
		t.Fatal("duplicate refreshes", resolves.Load())
	}
	// A second independent session is released while resolution is in flight.
	entered2 := make(chan struct{})
	gate2 := make(chan struct{})
	var secondResolves atomic.Int32
	s2 := New(func(ctx context.Context, _ string) (Source, error) {
		if secondResolves.Add(1) > 1 {
			close(entered2)
			<-gate2
		}
		return Source{URL: "https://media.bilivideo.com/a"}, nil
	}, http.DefaultClient)
	job2, _ := s2.Open(context.Background(), "BV17x411w7KC")
	s2.mu.Lock()
	observed := s2.sessions[job2.ID]
	s2.mu.Unlock()
	done := make(chan error, 1)
	go func() { _, err := s2.refreshSession(context.Background(), job2.ID, observed); done <- err }()
	<-entered2
	s2.Release(job2.ID)
	close(gate2)
	if err := <-done; !errors.Is(err, ErrExpired) {
		t.Fatal(err)
	}
	s2.mu.Lock()
	_, exists := s2.sessions[job2.ID]
	s2.mu.Unlock()
	if exists {
		t.Fatal("released session resurrected")
	}
}

func TestForbiddenAddressesRefreshOnlyOnceAndFailureCoolsDown(t *testing.T) {
	var resolves atomic.Int32
	s := New(func(context.Context, string) (Source, error) {
		count := resolves.Add(1)
		if count > 1 {
			return Source{}, ErrUnavailable
		}
		return Source{URL: "https://media.bilivideo.com/a?private=signed-secret"}, nil
	}, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) { return rangeResponse(403, "", "", "text/html"), nil })})
	job, _ := s.Open(context.Background(), "BV17x411w7KC")
	for range 2 {
		_, _, err := s.Fetch(context.Background(), job.ID, "GET", "bytes=2-5")
		if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "secret") {
			t.Fatal(err)
		}
	}
	if resolves.Load() != 2 {
		t.Fatal("refresh failure was hammered", resolves.Load())
	}
	// Successful renewal is used privately for the same byte range.
	var count atomic.Int32
	s2 := New(func(context.Context, string) (Source, error) {
		revision := count.Add(1)
		return Source{URL: "https://media.bilivideo.com/a?revision=" + strconv.Itoa(int(revision))}, nil
	}, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("revision") == "1" {
			return rangeResponse(403, "", "", "text/html"), nil
		}
		if r.Header.Get("Range") != "bytes=2-5" {
			t.Error("seek changed")
		}
		return rangeResponse(206, "bytes 2-5/10", "4", "video/mp4"), nil
	})})
	job2, _ := s2.Open(context.Background(), "BV17x411w7KC")
	response, release, err := s2.Fetch(context.Background(), job2.ID, "GET", "bytes=2-5")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	release()
	if count.Load() != 2 {
		t.Fatal(count.Load())
	}
}

func TestHeaderStallFallsBackAndCancellationReleasesCapacity(t *testing.T) {
	var requests atomic.Int32
	s := New(func(context.Context, string) (Source, error) {
		return Source{URL: "https://first.bilivideo.com/a", Backups: []string{"https://second.bilivideo.com/a"}}, nil
	}, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		if r.URL.Hostname() == "first.bilivideo.com" {
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		return rangeResponse(206, "bytes 2-5/10", "4", "video/mp4"), nil
	})})
	job, _ := s.Open(context.Background(), "BV17x411w7KC")
	started := time.Now()
	response, release, err := s.Fetch(context.Background(), job.ID, "GET", "bytes=2-5")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	release()
	if requests.Load() != 2 || time.Since(started) > 5*time.Second {
		t.Fatal("stalled header did not fall back")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = s.Fetch(ctx, job.ID, "GET", "bytes=2-5")
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(s.streaming) != 0 {
		t.Fatal("capacity leaked")
	}
}
