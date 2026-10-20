package playback

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func compatibilityWait(t *testing.T, s *Compatibility, id, state string) CompatibilityJob {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, err := s.Get(id)
		if err == nil && job.State == state {
			return job
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("compatibility job did not reach %s", state)
	return CompatibilityJob{}
}

func TestCompatibilityConvertsLocallyAndReleasesTemporaryMedia(t *testing.T) {
	jar, _ := cookiejar.New(nil)
	target, _ := url.Parse("https://media.xhscdn.com/fixture.mp4")
	jar.SetCookies(target, []*http.Cookie{{Name: "private", Value: "must-not-leak"}})
	client := &http.Client{Jar: jar, Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("credentials leaked to CDN")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("source")), ContentLength: 6, Header: make(http.Header)}, nil
	})}
	s, err := NewCompatibility(client, func(context.Context, string) (Source, error) {
		return Source{URL: target.String(), Duration: 47}, nil
	}, func(_ context.Context, input, output string) error {
		body, err := os.ReadFile(input)
		if err != nil || string(body) != "source" {
			return errors.New("unexpected conversion input")
		}
		return os.WriteFile(output, []byte("compatible video fixture"), 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	job, err := s.Open("private locator")
	if err != nil {
		t.Fatal(err)
	}
	compatibilityWait(t, s, job.ID, "ready")
	file, err := s.File(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	path := file.Name()
	body, _ := io.ReadAll(file)
	file.Close()
	if string(body) != "compatible video fixture" {
		t.Fatal("wrong media")
	}
	s.Release(job.ID)
	if _, err := s.Get(job.ID); !errors.Is(err, ErrExpired) {
		t.Fatal("released job remains visible", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("temporary media not removed")
	}
}

func TestCompatibilityRejectsUntrustedOversizedAndIncompleteSources(t *testing.T) {
	for _, scenario := range []string{"foreign", "redirect", "large", "truncated", "html", "long"} {
		t.Run(scenario, func(t *testing.T) {
			var conversions atomic.Int32
			client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				response := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("source")), ContentLength: 6, Header: make(http.Header), Request: r}
				switch scenario {
				case "redirect":
					response.StatusCode = 302
					response.Header.Set("Location", "https://127.0.0.1/private")
				case "large":
					response.ContentLength = maxCompatibilityInput + 1
				case "truncated":
					response.ContentLength = 20
				case "html":
					response.StatusCode = 403
				}
				return response, nil
			})}
			s, err := NewCompatibility(client, func(context.Context, string) (Source, error) {
				source := Source{URL: "https://media.xhscdn.com/video.mp4"}
				if scenario == "foreign" {
					source.URL = "https://media.xhscdn.com.evil.example/private"
				}
				if scenario == "long" {
					source.Duration = 1801
				}
				return source, nil
			}, func(context.Context, string, string) error { conversions.Add(1); return nil })
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			job, _ := s.Open("fixture")
			failed := compatibilityWait(t, s, job.ID, "failed")
			if conversions.Load() != 0 || strings.Contains(failed.Message, "private") {
				t.Fatal("invalid source reached converter or leaked details")
			}
			if _, err := s.File(job.ID); err == nil {
				t.Fatal("failed output served")
			}
		})
	}
}

func TestCompatibilityCancellationAndCloseStopWorker(t *testing.T) {
	entered := make(chan struct{})
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("source")), ContentLength: 6}, nil
	})}
	s, err := NewCompatibility(client, func(context.Context, string) (Source, error) {
		return Source{URL: "https://media.xhscdn.com/video.mp4"}, nil
	}, func(ctx context.Context, _, _ string) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	job, _ := s.Open("fixture")
	<-entered
	if _, err := s.Open("second"); !errors.Is(err, ErrBusy) {
		t.Fatal("unbounded concurrent conversion")
	}
	s.Release(job.ID)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.root); !os.IsNotExist(err) {
		t.Fatal("service temporary directory survived close")
	}
	if _, err := s.Open("late"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("closed service accepted work")
	}
}

func TestNeedsPlaybackConversion(t *testing.T) {
	for _, test := range []struct {
		name string
		info PlaybackFormat
		want bool
	}{
		{name: "h264 aac mp4", info: PlaybackFormat{Container: "mp4", VideoCodec: "h264", AudioCodec: "aac"}},
		{name: "hevc aac mp4", info: PlaybackFormat{Container: "mov,mp4,m4a,3gp,3g2,mj2", VideoCodec: "hevc", AudioCodec: "aac"}},
		{name: "webm", info: PlaybackFormat{Container: "webm", VideoCodec: "h264", AudioCodec: "aac"}, want: true},
		{name: "vp9", info: PlaybackFormat{Container: "mp4", VideoCodec: "vp9", AudioCodec: "aac"}, want: true},
		{name: "opus", info: PlaybackFormat{Container: "mp4", VideoCodec: "h264", AudioCodec: "opus"}, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := NeedsPlaybackConversion(test.info); got != test.want {
				t.Fatalf("NeedsPlaybackConversion(%+v) = %v, want %v", test.info, got, test.want)
			}
		})
	}
}

func TestCompatibilityAllowsDouyinCDNAndRejectsLookalikes(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want bool
	}{
		{raw: "https://v3.douyinvod.com/video.mp4", want: true},
		{raw: "https://cdn.douyinvod.net/video.mp4", want: true},
		{raw: "https://media.bytecdn.cn/video.mp4", want: true},
		{raw: "https://v2.zjcdn.com/video.mp4", want: true},
		{raw: "https://v3.amemv.com/video.mp4", want: true},
		{raw: "https://douyinvod.com/video.mp4", want: false},
		{raw: "https://douyinvod.com.evil.example/video.mp4", want: false},
		{raw: "http://v3.douyinvod.com/video.mp4", want: false},
	} {
		if got := ValidCompatibilitySource(test.raw); got != test.want {
			t.Fatalf("ValidCompatibilitySource(%q) = %v, want %v", test.raw, got, test.want)
		}
	}
}

func TestCompatibilitySkipsConversionForCompatibleMP4(t *testing.T) {
	var converted bool
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Referer") != "https://www.douyin.com/" {
			t.Errorf("missing Douyin referer: %q", r.Header.Get("Referer"))
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("source")), ContentLength: 6, Header: make(http.Header)}, nil
	})}
	s, err := NewCompatibilityWithProbe(client, func(context.Context, string) (Source, error) {
		return Source{URL: "https://v3.douyinvod.com/video.mp4"}, nil
	}, func(context.Context, string, string) error {
		converted = true
		return errors.New("conversion should be skipped")
	}, func(context.Context, string) (PlaybackFormat, error) {
		return PlaybackFormat{Container: "mp4", VideoCodec: "h264", AudioCodec: "aac"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	job, err := s.Open("https://www.douyin.com/video/1234567890123456")
	if err != nil {
		t.Fatal(err)
	}
	compatibilityWait(t, s, job.ID, "ready")
	if converted {
		t.Fatal("compatible MP4 was sent to converter")
	}
}
