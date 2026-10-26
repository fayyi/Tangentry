package sqlite

import (
	"context"
	"sync"
	"testing"
	"time"

	"tangentry/internal/agent"
)

func TestAgentReviewIsIdempotentUnderConcurrentWrites(t *testing.T) {
	store, err := Open(t.TempDir() + "/agent.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	event := agent.ReviewEvent{ID: "first", IdempotencyKey: "run:one", ContentID: "youtube:abcdefghijk", Title: "title", Status: "selected", Rating: agent.RatingCanWatch, CreatedAt: 1}
	const callers = 12
	var wait sync.WaitGroup
	wait.Add(callers)
	duplicates := make(chan bool, callers)
	errors := make(chan error, callers)
	for index := 0; index < callers; index++ {
		go func(index int) {
			defer wait.Done()
			candidate := event
			candidate.ID = string(rune('a' + index))
			result, duplicate, err := store.RecordReview(context.Background(), candidate)
			if err != nil {
				errors <- err
				return
			}
			if result.IdempotencyKey != event.IdempotencyKey {
				errors <- err
				return
			}
			duplicates <- duplicate
		}(index)
	}
	wait.Wait()
	close(duplicates)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	inserted := 0
	for duplicate := range duplicates {
		if !duplicate {
			inserted++
		}
	}
	if inserted != 1 {
		t.Fatalf("non-duplicate insertions = %d", inserted)
	}
	rows, err := store.ListReviews(context.Background(), event.ContentID, 20)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %d, error = %v", len(rows), err)
	}
}

func TestAgentOperationPersistsAndRecoversOnRestart(t *testing.T) {
	path := t.TempDir() + "/agent.db"
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	operation := agent.Operation{ID: "operation-one", RunID: "run-operation", Action: "transcribe_content", ContentID: "youtube:abcdefghijk", State: "running", Source: "local_transcription", CostClass: "local", EstimatedDuration: "minutes", StartedAt: time.Unix(10, 0)}
	if err := store.CreateOperation(context.Background(), operation); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.RecoverOperations(context.Background()); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.GetOperation(context.Background(), operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.State != "failed" || recovered.RunID != operation.RunID || recovered.Error == "" || recovered.FinishedAt.IsZero() {
		t.Fatalf("recovered operation = %+v", recovered)
	}
}

func TestMigrationRedactsLegacyAgentOperationErrors(t *testing.T) {
	store, err := Open(t.TempDir() + "/agent-operation-redaction.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	for _, operation := range []agent.Operation{
		{ID: "failed-legacy", Action: "transcribe_content", ContentID: "youtube:abcdefghijk", State: "failed", Error: "/private/path/token.json", StartedAt: time.Unix(1, 0)},
		{ID: "unsupported-legacy", Action: "transcribe_content", ContentID: "youtube:abcdefghijk", State: "unsupported", Error: "/private/path/token.json", StartedAt: time.Unix(1, 0)},
	} {
		if err := store.CreateOperation(ctx, operation); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.db.Exec(`DELETE FROM schema_version WHERE version=79`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(store.db); err != nil {
		t.Fatal(err)
	}
	failed, err := store.GetOperation(ctx, "failed-legacy")
	if err != nil || failed.Error != "操作失败；详细错误未保存。" {
		t.Fatalf("redacted failed operation = %+v, error = %v", failed, err)
	}
	unsupported, err := store.GetOperation(ctx, "unsupported-legacy")
	if err != nil || unsupported.Error != "当前能力不可用。" {
		t.Fatalf("redacted unsupported operation = %+v, error = %v", unsupported, err)
	}
}

func TestAgentActivityListsRecentRunsAndAudit(t *testing.T) {
	store, err := Open(t.TempDir() + "/agent-activity.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.CreateRun(ctx, agent.Run{ID: "run-one", Client: "desktop-agent", Trigger: "daily", State: "running", StartedAt: 10}); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishRun(ctx, "run-one", "completed", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordAudit(ctx, agent.AuditEvent{RunID: "run-one", Action: "search_content", NetworkFetch: true, Outcome: "ok", CreatedAt: 20}); err != nil {
		t.Fatal(err)
	}
	runs, err := store.ListRuns(ctx, 5)
	if err != nil || len(runs) != 1 || runs[0].State != "completed" || runs[0].FinishedAt == 0 {
		t.Fatalf("runs = %+v, error = %v", runs, err)
	}
	events, err := store.ListAudit(ctx, 5)
	if err != nil || len(events) != 1 || events[0].Action != "search_content" || !events[0].NetworkFetch {
		t.Fatalf("audit = %+v, error = %v", events, err)
	}
}

func TestAgentReportsCanBeListedByKindAndDate(t *testing.T) {
	store, err := Open(t.TempDir() + "/agent-reports.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	for _, report := range []agent.Report{
		{ID: "daily-1", Date: "2026-10-25", Kind: "daily", Markdown: "daily old", CreatedAt: 1},
		{ID: "digest-1", Date: "2026-10-01", Kind: "digest", Markdown: "digest", CreatedAt: 2},
		{ID: "daily-2", Date: "2026-10-26", Kind: "daily", Markdown: "daily new", CreatedAt: 3},
	} {
		if _, err := store.AppendReport(ctx, report); err != nil {
			t.Fatal(err)
		}
	}
	reports, err := store.ListReports(ctx, agent.ReportQuery{Kind: "daily", Limit: 10})
	if err != nil || len(reports) != 2 || reports[0].ID != "daily-2" {
		t.Fatalf("daily reports = %+v, error = %v", reports, err)
	}
	reports, err = store.ListReports(ctx, agent.ReportQuery{Date: "2026-10-01", Limit: 10})
	if err != nil || len(reports) != 1 || reports[0].Kind != "digest" {
		t.Fatalf("date-filtered reports = %+v, error = %v", reports, err)
	}
}

func TestAgentAuditCanBeCorrelatedToRun(t *testing.T) {
	store, err := Open(t.TempDir() + "/agent-audit-run.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.RecordAudit(ctx, agent.AuditEvent{RunID: "run-correlated", Action: "get_subtitles", Outcome: "ok", CreatedAt: 30}); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListAudit(ctx, 10)
	if err != nil || len(events) != 1 || events[0].RunID != "run-correlated" {
		t.Fatalf("audit events = %+v, error = %v", events, err)
	}
}
