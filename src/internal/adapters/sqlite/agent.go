package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"tangentry/internal/agent"
)

func (s *Store) CreateRun(ctx context.Context, run agent.Run) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO agent_runs(id,client,trigger_kind,state,started_at) VALUES(?,?,?,?,?)`, run.ID, run.Client, run.Trigger, run.State, run.StartedAt)
	return err
}

func (s *Store) FinishRun(ctx context.Context, id, state, errText string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE agent_runs SET state=?,finished_at=?,error=? WHERE id=?`, state, time.Now().Unix(), errText, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) RecordReview(ctx context.Context, event agent.ReviewEvent) (agent.ReviewEvent, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return agent.ReviewEvent{}, false, err
	}
	defer tx.Rollback()
	var existing agent.ReviewEvent
	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO agent_review_events(id,idempotency_key,run_id,content_id,title,author,status,rating,reason,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, event.ID, event.IdempotencyKey, event.RunID, event.ContentID, event.Title, event.Author, event.Status, event.Rating, event.Reason, event.CreatedAt)
	if err != nil {
		return agent.ReviewEvent{}, false, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return agent.ReviewEvent{}, false, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT id,idempotency_key,run_id,content_id,title,author,status,rating,reason,created_at FROM agent_review_events WHERE idempotency_key=?`, event.IdempotencyKey).Scan(&existing.ID, &existing.IdempotencyKey, &existing.RunID, &existing.ContentID, &existing.Title, &existing.Author, &existing.Status, &existing.Rating, &existing.Reason, &existing.CreatedAt); err != nil {
		return agent.ReviewEvent{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return agent.ReviewEvent{}, false, err
	}
	return existing, inserted == 0, nil
}

func (s *Store) AppendReport(ctx context.Context, report agent.Report) (agent.Report, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO agent_reports(id,report_date,kind,markdown,created_at) VALUES(?,?,?,?,?)`, report.ID, report.Date, report.Kind, report.Markdown, report.CreatedAt)
	return report, err
}

func (s *Store) ListReports(ctx context.Context, query agent.ReportQuery) ([]agent.Report, error) {
	sqlQuery := `SELECT id,report_date,kind,markdown,created_at FROM agent_reports`
	args := make([]any, 0, 3)
	filters := make([]string, 0, 2)
	if query.Kind != "" {
		filters = append(filters, "kind=?")
		args = append(args, query.Kind)
	}
	if query.Date != "" {
		filters = append(filters, "report_date=?")
		args = append(args, query.Date)
	}
	if len(filters) > 0 {
		sqlQuery += " WHERE " + strings.Join(filters, " AND ")
	}
	sqlQuery += " ORDER BY report_date DESC, created_at DESC, id DESC LIMIT ?"
	args = append(args, query.Limit)
	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]agent.Report, 0, query.Limit)
	for rows.Next() {
		var item agent.Report
		if err := rows.Scan(&item.ID, &item.Date, &item.Kind, &item.Markdown, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) SaveNoteDraft(ctx context.Context, draft agent.NoteDraft) (agent.NoteDraft, error) {
	payload, err := json.Marshal(draft)
	if err != nil {
		return agent.NoteDraft{}, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO agent_note_drafts(id,content_id,payload,state,created_at,updated_at) VALUES(?,?,?,?,?,?)`, draft.ID, draft.ContentID, string(payload), draft.State, draft.CreatedAt, draft.UpdatedAt)
	return draft, err
}

func (s *Store) RecordAudit(ctx context.Context, event agent.AuditEvent) error {
	id, err := agent.NewID()
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO agent_audit_events(id,run_id,action,network_fetch,external_processing,outcome,created_at) VALUES(?,?,?,?,?,?,?)`, id, event.RunID, event.Action, boolInt(event.NetworkFetch), boolInt(event.ExternalProcess), event.Outcome, event.CreatedAt)
	return err
}

func (s *Store) ListReviews(ctx context.Context, contentID string, limit int) ([]agent.ReviewEvent, error) {
	query := `SELECT id,idempotency_key,run_id,content_id,title,author,status,rating,reason,created_at FROM agent_review_events`
	args := []any{}
	if contentID != "" {
		query += ` WHERE content_id=?`
		args = append(args, contentID)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]agent.ReviewEvent, 0)
	for rows.Next() {
		var item agent.ReviewEvent
		if err := rows.Scan(&item.ID, &item.IdempotencyKey, &item.RunID, &item.ContentID, &item.Title, &item.Author, &item.Status, &item.Rating, &item.Reason, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListRuns(ctx context.Context, limit int) ([]agent.Run, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,client,trigger_kind,state,started_at,finished_at,error FROM agent_runs ORDER BY started_at DESC,id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := make([]agent.Run, 0)
	for rows.Next() {
		var run agent.Run
		if err := rows.Scan(&run.ID, &run.Client, &run.Trigger, &run.State, &run.StartedAt, &run.FinishedAt, &run.Error); err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (s *Store) ListAudit(ctx context.Context, limit int) ([]agent.AuditEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT run_id,action,network_fetch,external_processing,outcome,created_at FROM agent_audit_events ORDER BY created_at DESC,id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]agent.AuditEvent, 0)
	for rows.Next() {
		var event agent.AuditEvent
		var network, external int
		if err := rows.Scan(&event.RunID, &event.Action, &network, &external, &event.Outcome, &event.CreatedAt); err != nil {
			return nil, err
		}
		event.NetworkFetch = network != 0
		event.ExternalProcess = external != 0
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) CreateOperation(ctx context.Context, operation agent.Operation) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO agent_operations(id,run_id,action,content_id,state,source,cost_class,estimated_duration,external_processing,started_at,finished_at,error) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		operation.ID, operation.RunID, operation.Action, operation.ContentID, operation.State, operation.Source, operation.CostClass, operation.EstimatedDuration, boolInt(operation.ExternalProcessing), unixTime(operation.StartedAt), unixTime(operation.FinishedAt), operation.Error)
	return err
}

func (s *Store) UpdateOperation(ctx context.Context, operation agent.Operation) error {
	result, err := s.db.ExecContext(ctx, `UPDATE agent_operations SET state=?,source=?,cost_class=?,estimated_duration=?,external_processing=?,finished_at=?,error=? WHERE id=?`,
		operation.State, operation.Source, operation.CostClass, operation.EstimatedDuration, boolInt(operation.ExternalProcessing), unixTime(operation.FinishedAt), operation.Error, operation.ID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return agent.ErrInvalid
	}
	return nil
}

func (s *Store) GetOperation(ctx context.Context, id string) (agent.Operation, error) {
	var operation agent.Operation
	var external int
	var started, finished int64
	err := s.db.QueryRowContext(ctx, `SELECT id,run_id,action,content_id,state,source,cost_class,estimated_duration,external_processing,started_at,finished_at,error FROM agent_operations WHERE id=?`, id).Scan(
		&operation.ID, &operation.RunID, &operation.Action, &operation.ContentID, &operation.State, &operation.Source, &operation.CostClass, &operation.EstimatedDuration, &external, &started, &finished, &operation.Error)
	if err == sql.ErrNoRows {
		return agent.Operation{}, agent.ErrInvalid
	}
	if err != nil {
		return agent.Operation{}, err
	}
	operation.ExternalProcessing = external != 0
	operation.StartedAt = time.Unix(started, 0)
	operation.FinishedAt = unixToTime(finished)
	return operation, nil
}

func (s *Store) RecoverOperations(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE agent_operations SET state='failed',finished_at=?,error='Tangentry 在操作完成前退出，操作未恢复。' WHERE state IN ('queued','running')`, time.Now().Unix())
	return err
}

func unixTime(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.Unix()
}

func unixToTime(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	return time.Unix(value, 0)
}

var _ agent.Repository = (*Store)(nil)
var _ agent.OperationRepository = (*Store)(nil)
var _ agent.ActivityRepository = (*Store)(nil)
