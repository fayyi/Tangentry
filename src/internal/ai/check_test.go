package ai

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

type checkProvider struct {
	translatorFunc
	summaryProviderFunc
}

func (checkProvider) SupportsSummary(c Config) bool { return c.Model != "translation-only" }

func TestCheckRoutesTasksWithoutChangingSettingsOrCaches(t *testing.T) {
	var translationCalls, summaryCalls int
	provider := checkProvider{
		translatorFunc: translatorFunc(func(ctx context.Context, c Config, input []Segment) ([]Segment, error) {
			translationCalls++
			deadline, ok := ctx.Deadline()
			if c.Model != "translation-only" || !ok || time.Until(deadline) > 30*time.Second || len(input) != 1 || input[0].Index != 0 || input[0].Text != "The reader keeps your notes with the original article." {
				t.Error("check did not use fixed bounded translation input")
			}
			return []Segment{{Index: 0, Text: "测试译文"}}, nil
		}),
		summaryProviderFunc: summaryProviderFunc(func(_ context.Context, c Config, input SummaryRequest) (SummaryDraft, error) {
			summaryCalls++
			if c.Model != "summary-model" || input.Mode != "leaf" || len(input.Segments) != 1 || input.Segments[0].Index != 0 || input.Segments[0].Text != "The reader keeps your notes with the original article." {
				t.Error("check did not use fixed summary input and task model")
			}
			return SummaryDraft{Overview: "测试概述", Points: []SummaryPoint{{Text: "测试要点", Indices: []int{0}}}}, nil
		}),
	}
	s, _, root := configured(t, provider)
	summaryModel := "summary-model"
	status, err := s.Configure(ConfigureInput{BaseURL: s.Status().BaseURL, Model: "translation-only", SummaryModel: &summaryModel, Revision: s.Status().Revision})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(root, "settings.json"))
	for _, task := range []string{"translation", "summary"} {
		result, err := s.Check(context.Background(), CheckInput{Task: task, Revision: status.Revision})
		expectedModel := "translation-only"
		if task == "summary" {
			expectedModel = summaryModel
		}
		if err != nil || result.Task != task || result.Model != expectedModel || result.Revision != status.Revision || result.ElapsedMS < 0 {
			t.Fatal("unexpected check result", err, result)
		}
	}
	after, _ := os.ReadFile(filepath.Join(root, "settings.json"))
	if storeFor(root).Count() != 0 || !reflect.DeepEqual(before, after) || s.Status().Translating || translationCalls != 1 || summaryCalls != 1 {
		t.Fatal("check changed state or made extra calls")
	}
}

func TestCheckRejectsInvalidStaleUnsupportedAndMalformedOutput(t *testing.T) {
	calls := 0
	s, in, _ := configured(t, translatorFunc(func(context.Context, Config, []Segment) ([]Segment, error) {
		calls++
		return []Segment{{Index: 99, Text: "wrong index"}}, nil
	}))
	for _, tc := range []struct {
		input CheckInput
		err   error
	}{
		{CheckInput{Task: "unknown", Revision: in.Revision}, ErrInvalid},
		{CheckInput{Task: "translation", Revision: in.Revision - 1}, ErrConflict},
		{CheckInput{Task: "summary", Revision: in.Revision}, ErrNotReady},
	} {
		if _, err := s.Check(context.Background(), tc.input); !errors.Is(err, tc.err) {
			t.Fatal(tc.input, err)
		}
	}
	if calls != 0 {
		t.Fatal("invalid check invoked provider")
	}
	if _, err := s.Check(context.Background(), CheckInput{Task: "translation", Revision: in.Revision}); !errors.Is(err, ErrFormat) || calls != 1 || s.Status().Translating {
		t.Fatal("invalid output accepted or lock retained", err)
	}
}

func TestCheckCancellationAndCloseReleaseTheSharedOperation(t *testing.T) {
	for _, closing := range []bool{false, true} {
		entered := make(chan struct{})
		var calls atomic.Int64
		s, in, _ := configured(t, translatorFunc(func(ctx context.Context, _ Config, _ []Segment) ([]Segment, error) {
			if calls.Add(1) > 1 {
				return []Segment{{Index: 0, Text: "有效译文"}}, nil
			}
			close(entered)
			<-ctx.Done()
			return nil, ctx.Err()
		}))
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { _, err := s.Check(ctx, CheckInput{Task: "translation", Revision: in.Revision}); done <- err }()
		<-entered
		if !s.Status().Translating {
			t.Fatal("active check was not visible")
		}
		if _, err := s.Check(context.Background(), CheckInput{Task: "translation", Revision: in.Revision}); !errors.Is(err, ErrBusy) {
			t.Fatal("concurrent check accepted", err)
		}
		if _, err := s.Translate(context.Background(), in); !errors.Is(err, ErrBusy) {
			t.Fatal("translation overlapped check", err)
		}
		if _, err := s.Configure(ConfigureInput{BaseURL: s.Status().BaseURL, Model: "other", Revision: in.Revision}); !errors.Is(err, ErrBusy) {
			t.Fatal("configuration changed during check", err)
		}
		if closing {
			s.Close()
		} else {
			cancel()
		}
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) || s.Status().Translating {
				t.Fatal("check did not release after cancellation", err)
			}
		case <-time.After(time.Second):
			t.Fatal("check did not cancel")
		}
		cancel()
		if !closing {
			if _, err := s.Check(context.Background(), CheckInput{Task: "translation", Revision: in.Revision}); err != nil {
				t.Fatal("subsequent check blocked", err)
			}
		}
	}
}
