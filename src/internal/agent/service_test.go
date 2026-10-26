package agent

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"tangentry/internal/notes"
	"tangentry/internal/subscriptions"
)

type testProvider struct{}

func (testProvider) Capabilities(context.Context) CapabilityMatrix {
	return CapabilityMatrix{ProtocolVersion: "1"}
}
func (testProvider) Search(context.Context, string, string, string, bool) (SearchResult, error) {
	return SearchResult{}, nil
}
func (testProvider) Content(context.Context, string, bool) (ContentResult, error) {
	return ContentResult{}, nil
}
func (testProvider) Inbox(context.Context, subscriptions.InboxQuery) (InboxResult, error) {
	return InboxResult{}, nil
}
func (testProvider) Subtitles(context.Context, string, string, bool) (SubtitleResult, error) {
	return SubtitleResult{}, nil
}
func (testProvider) Notes(context.Context, string, string) ([]notes.Note, error) { return nil, nil }
func (testProvider) Social(context.Context, string) (SocialResult, error)        { return SocialResult{}, nil }
func (testProvider) CaptureFrames(context.Context, string, []float64) ([]AgentImage, error) {
	return nil, nil
}

type testRepo struct{}

func (testRepo) CreateRun(context.Context, Run) error                    { return nil }
func (testRepo) FinishRun(context.Context, string, string, string) error { return nil }
func (testRepo) RecordReview(context.Context, ReviewEvent) (ReviewEvent, bool, error) {
	return ReviewEvent{}, false, nil
}
func (testRepo) AppendReport(context.Context, Report) (Report, error)            { return Report{}, nil }
func (testRepo) SaveNoteDraft(context.Context, NoteDraft) (NoteDraft, error)     { return NoteDraft{}, nil }
func (testRepo) RecordAudit(context.Context, AuditEvent) error                   { return nil }
func (testRepo) ListReviews(context.Context, string, int) ([]ReviewEvent, error) { return nil, nil }

type recordingRepo struct {
	testRepo
	event ReviewEvent
}

type derivedTestProvider struct {
	testProvider
	plan DerivedPlan
}

func (provider derivedTestProvider) PrepareDerived(context.Context, string, DerivedRequest) (DerivedPlan, error) {
	return provider.plan, nil
}

type operationTestRepo struct {
	testRepo
	mu               sync.Mutex
	operations       map[string]Operation
	runningPersisted chan struct{}
	releaseRunning   chan struct{}
}

func (repo *operationTestRepo) CreateOperation(_ context.Context, operation Operation) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.operations == nil {
		repo.operations = make(map[string]Operation)
	}
	repo.operations[operation.ID] = operation
	return nil
}

func (repo *operationTestRepo) UpdateOperation(_ context.Context, operation Operation) error {
	repo.mu.Lock()
	if repo.operations == nil {
		repo.operations = make(map[string]Operation)
	}
	repo.operations[operation.ID] = operation
	repo.mu.Unlock()
	if operation.State == "running" && repo.runningPersisted != nil {
		select {
		case repo.runningPersisted <- struct{}{}:
		default:
		}
		<-repo.releaseRunning
	}
	return nil
}

func (repo *operationTestRepo) GetOperation(_ context.Context, id string) (Operation, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	operation, ok := repo.operations[id]
	if !ok {
		return Operation{}, ErrInvalid
	}
	return operation, nil
}

func (*operationTestRepo) RecoverOperations(context.Context) error { return nil }

func (repo *recordingRepo) RecordReview(_ context.Context, event ReviewEvent) (ReviewEvent, bool, error) {
	repo.event = event
	return event, false, nil
}

func TestReportEscapesUntrustedMarkdown(t *testing.T) {
	service := New(testProvider{}, testRepo{})
	report, err := service.SaveReport(context.Background(), ReportInput{Date: "2026-10-26", Entries: []ReportEntry{{ContentID: "youtube:abcdefghijk", Title: "# injected\n## heading", Rating: RatingCanWatch, Reason: "[x](javascript:bad)", Status: "selected"}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(report.Markdown, "\n## heading") || strings.Contains(report.Markdown, "[x](javascript") {
		t.Fatalf("untrusted text escaped into markdown: %q", report.Markdown)
	}
}

func TestCaptureFramesRejectsNonFinitePosition(t *testing.T) {
	service := New(testProvider{}, testRepo{})
	if _, err := service.CaptureFrames(context.Background(), "youtube:abcdefghijk", []float64{math.NaN()}); err != ErrInvalid {
		t.Fatalf("expected invalid position, got %v", err)
	}
}

func TestRunIDContextCarriesOnlyCorrelationValue(t *testing.T) {
	ctx := WithRunID(context.Background(), "run-123")
	if RunID(ctx) != "run-123" || RunID(context.Background()) != "" {
		t.Fatalf("run IDs: contextual=%q empty=%q", RunID(ctx), RunID(context.Background()))
	}
}

func TestRecordReviewInheritsRunIDFromContext(t *testing.T) {
	repo := &recordingRepo{}
	service := New(testProvider{}, repo)
	_, _, err := service.RecordReview(WithRunID(context.Background(), "run-ctx"), ReviewInput{
		IdempotencyKey: "review-1",
		ContentID:      "youtube:abcdefghijk",
		Title:          "title",
		Status:         "selected",
		Rating:         RatingCanWatch,
	})
	if err != nil {
		t.Fatal(err)
	}
	if repo.event.RunID != "run-ctx" {
		t.Fatalf("review run id = %q", repo.event.RunID)
	}
}

func TestDerivedFailureDoesNotPersistUnderlyingError(t *testing.T) {
	repo := &operationTestRepo{}
	finished := make(chan struct{})
	service := New(derivedTestProvider{plan: DerivedPlan{
		Source: "local_transcription", CostClass: "local", EstimatedDuration: "minutes",
		Run: func(context.Context) error {
			defer close(finished)
			return errors.New("failed at /Users/private/credentials/token.json")
		},
	}}, repo)
	defer service.Close()

	started, err := service.StartDerived(WithRunID(context.Background(), "run-derived"), "transcribe_content", DerivedRequest{ContentID: "youtube:abcdefghijk"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("derived operation did not finish")
	}
	operation := waitForOperationState(t, service, started.ID, "failed")
	if operation.Error != "操作失败；详细错误未保存。" || strings.Contains(operation.Error, "credentials") {
		t.Fatalf("operation error was not sanitized: %q", operation.Error)
	}
	persisted, err := repo.GetOperation(context.Background(), started.ID)
	if err != nil || persisted.Error != operation.Error {
		t.Fatalf("persisted operation = %+v, error = %v", persisted, err)
	}
}

func TestCancellationBeforeDerivedRunnerStartsSkipsProviderWork(t *testing.T) {
	repo := &operationTestRepo{runningPersisted: make(chan struct{}, 1), releaseRunning: make(chan struct{})}
	var ran bool
	service := New(derivedTestProvider{plan: DerivedPlan{
		Source: "local_transcription", CostClass: "local", EstimatedDuration: "minutes",
		Run: func(context.Context) error {
			ran = true
			return nil
		},
	}}, repo)
	defer service.Close()

	started, err := service.StartDerived(context.Background(), "transcribe_content", DerivedRequest{ContentID: "youtube:abcdefghijk"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-repo.runningPersisted:
	case <-time.After(2 * time.Second):
		t.Fatal("operation did not enter its running transition")
	}
	if err := service.CancelOperation(context.Background(), started.ID); err != nil {
		t.Fatal(err)
	}
	close(repo.releaseRunning)
	operation := waitForOperationState(t, service, started.ID, "cancelled")
	if ran {
		t.Fatal("cancelled operation executed provider work")
	}
	if operation.Error != "操作已取消。" {
		t.Fatalf("cancelled operation error = %q", operation.Error)
	}
}

func waitForOperationState(t *testing.T, service *Service, id, state string) Operation {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		operation, err := service.Operation(context.Background(), id)
		if err == nil && operation.State == state {
			return operation
		}
		time.Sleep(time.Millisecond)
	}
	operation, err := service.Operation(context.Background(), id)
	t.Fatalf("operation did not reach %q: %+v (%v)", state, operation, err)
	return Operation{}
}
