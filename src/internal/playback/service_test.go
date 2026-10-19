package playback

import (
	"context"
	"encoding/json"
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

func TestDouyinRedirectIsPlatformBoundAndKeepsReferer(t *testing.T) {
	var hosts []string
	s := New(func(context.Context, string) (Source, error) {
		return Source{URL: "https://v1.douyinvod.com/start.mp4", Platform: "douyin", Referer: "https://www.douyin.com/video/7340916925458779430"}, nil
	}, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		hosts = append(hosts, r.URL.Hostname())
		if r.Header.Get("Referer") != "https://www.douyin.com/video/7340916925458779430" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Fatalf("redirect request headers: %+v", r.Header)
		}
		if r.URL.Path == "/start.mp4" {
			return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {"https://v1.douyinvod.com/final.mp4"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
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
	if len(hosts) != 2 || hosts[0] != "v1.douyinvod.com" || hosts[1] != "v1.douyinvod.com" {
		t.Fatalf("requested hosts = %v", hosts)
	}

	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {"https://media.bilivideo.com/escape.mp4"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	blocked := New(func(context.Context, string) (Source, error) {
		return Source{URL: "https://v1.douyinvod.com/start.mp4", Platform: "douyin"}, nil
	}, client)
	blockedJob, _ := blocked.OpenDouyin(context.Background(), "7340916925458779430")
	if _, _, err := blocked.Fetch(context.Background(), blockedJob.ID, "GET", ""); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("cross-platform redirect accepted: %v", err)
	}
}

func TestDouyinMediaFailuresKeepSanitizedStatusAndRefreshDiagnostics(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusGone, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			trace := NewDiagnosticTrace()
			page := "https://www.douyin.com/video/7340916925458779430"
			client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader("fixture"))}, nil
			})}
			service := New(func(context.Context, string) (Source, error) {
				return Source{URL: "https://v1.douyinvod.com/video.mp4?msToken=fixture", Platform: "douyin", Referer: page}, nil
			}, client)
			ctx := ContextWithDiagnostics(context.Background(), trace)
			session, err := service.OpenDouyin(ctx, "7340916925458779430")
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = service.Fetch(context.Background(), session.ID, http.MethodGet, "")
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("fetch error classification = %v", err)
			}
			var diagnostic *DiagnosticError
			if !errors.As(err, &diagnostic) {
				t.Fatalf("diagnostic wrapper missing: %v", err)
			}
			stages := diagnostic.Diagnostics().Stages
			found := false
			for _, stage := range stages {
				if stage.Stage == "session_media_fetch" && stage.HTTPStatus != nil && *stage.HTTPStatus == status && stage.ContentType != nil && *stage.ContentType == "text/html" {
					found = true
				}
			}
			if !found || strings.Contains(err.Error(), "msToken") || strings.Contains(strings.Join(stageStrings(stages), " "), "fixture") {
				t.Fatalf("unsafe or incomplete media diagnostics: %v %+v", err, stages)
			}
		})
	}
}

func TestDouyinFrontendMediaReportRedactsTextAndLogsFinalStageOnce(t *testing.T) {
	service := New(func(context.Context, string) (Source, error) {
		return Source{URL: "https://v1.douyinvod.com/video.mp4", Platform: "douyin"}, nil
	}, &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		return rangeResponse(http.StatusOK, "", "", "video/mp4"), nil
	})})
	trace := NewDiagnosticTrace()
	session, err := service.OpenDouyin(ContextWithDiagnostics(context.Background(), trace), "7340916925458779430")
	if err != nil {
		t.Fatal(err)
	}
	report := service.ReportFrontendMedia(session.ID, 4, "media failed token=private a_bogus=signed-private", 1)
	if report.FinalOutcome != OutcomeException {
		t.Fatalf("final outcome = %s", report.FinalOutcome)
	}
	encoded, _ := json.Marshal(report)
	if strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "signed-private") {
		t.Fatalf("frontend message leaked sensitive value: %s", encoded)
	}
	if trace.Snapshot().Stages[len(trace.Snapshot().Stages)-1].Stage != "frontend_media" || !trace.IsLogged() {
		t.Fatal("frontend failure did not close the attempt")
	}
	if trace.Snapshot().FinalOutcome != OutcomeException {
		t.Fatalf("frontend failure final outcome = %s", trace.Snapshot().FinalOutcome)
	}
}

func TestDouyinFrontendMediaReadyDoesNotCloseTraceThroughCodeZeroReport(t *testing.T) {
	service := New(func(context.Context, string) (Source, error) {
		return Source{URL: "https://v1.douyinvod.com/video.mp4", Platform: "douyin"}, nil
	}, &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		return rangeResponse(http.StatusOK, "", "", "video/mp4"), nil
	})})
	trace := NewDiagnosticTrace()
	session, err := service.OpenDouyin(ContextWithDiagnostics(context.Background(), trace), "7340916925458779430")
	if err != nil {
		t.Fatal(err)
	}
	readyReport := service.ReportFrontendMedia(session.ID, 0, "", 0)
	if readyReport.FinalOutcome != "" || trace.IsLogged() {
		t.Fatalf("code-zero report closed trace: %+v logged=%v", readyReport, trace.IsLogged())
	}
	failed := service.ReportFrontendMedia(session.ID, 4, "media failed", 1)
	if failed.FinalOutcome != OutcomeException || !trace.IsLogged() {
		t.Fatalf("later media failure did not close trace: %+v logged=%v", failed, trace.IsLogged())
	}

	trace = NewDiagnosticTrace()
	session, err = service.OpenDouyin(ContextWithDiagnostics(context.Background(), trace), "7340916925458779430")
	if err != nil {
		t.Fatal(err)
	}
	failed = service.ReportFrontendMediaFailure(session.ID, 0, "", 0)
	if failed.FinalOutcome != OutcomeException || !trace.IsLogged() {
		t.Fatalf("code-zero media failure did not close trace: %+v logged=%v", failed, trace.IsLogged())
	}
	last := failed.Stages[len(failed.Stages)-1]
	if last.Stage != "frontend_media" || last.Outcome != OutcomeException || last.FrontendMediaCode != nil {
		t.Fatalf("code-zero failure stage = %+v", last)
	}

	trace = NewDiagnosticTrace()
	session, err = service.OpenDouyin(ContextWithDiagnostics(context.Background(), trace), "7340916925458779430")
	if err != nil {
		t.Fatal(err)
	}
	ready := service.ReportMediaReady(session.ID)
	if ready.FinalOutcome != OutcomeOK || !trace.IsLogged() {
		t.Fatalf("explicit ready signal did not log success: %+v logged=%v", ready, trace.IsLogged())
	}
}

func stageStrings(stages []DiagnosticStage) []string {
	values := make([]string, 0, len(stages))
	for _, stage := range stages {
		encoded, _ := json.Marshal(stage)
		values = append(values, string(encoded))
	}
	return values
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
