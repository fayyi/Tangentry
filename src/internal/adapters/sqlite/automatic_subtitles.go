package sqlite

import (
	"context"
	"encoding/json"
	"errors"

	"tangentry/internal/automaticsubtitles"
)

func (s *Store) ListAutomaticSessions(ctx context.Context, contentID string) ([]automaticsubtitles.StoredSession, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,content_id,language,capability,state,processed_seconds,furthest_position,duration_seconds,job_id,error_code,error_message,draft_payload,revision,updated_at FROM automatic_subtitle_sessions WHERE content_id=? ORDER BY updated_at DESC`, contentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]automaticsubtitles.StoredSession, 0)
	for rows.Next() {
		value, err := scanAutomaticSession(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) GetAutomaticSession(ctx context.Context, id string) (automaticsubtitles.StoredSession, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,content_id,language,capability,state,processed_seconds,furthest_position,duration_seconds,job_id,error_code,error_message,draft_payload,revision,updated_at FROM automatic_subtitle_sessions WHERE id=?`, id)
	return scanAutomaticSession(row)
}

type automaticScanner interface{ Scan(...any) error }

func scanAutomaticSession(scanner automaticScanner) (automaticsubtitles.StoredSession, error) {
	var value automaticsubtitles.StoredSession
	var state, draft string
	if err := scanner.Scan(&value.Session.ID, &value.Session.ContentID, &value.Session.Language, &value.Session.Capability, &state, &value.Session.ProcessedSeconds, &value.Session.FurthestPosition, &value.Session.DurationSeconds, &value.Session.JobID, &value.Session.ErrorCode, &value.Session.ErrorMessage, &draft, &value.Revision, &value.Session.UpdatedAt); err != nil {
		return automaticsubtitles.StoredSession{}, err
	}
	value.Session.State = automaticsubtitles.State(state)
	if draft != "" {
		if err := json.Unmarshal([]byte(draft), &value.Draft); err != nil {
			return automaticsubtitles.StoredSession{}, err
		}
	}
	return value, nil
}

func (s *Store) SaveAutomaticSession(ctx context.Context, value automaticsubtitles.StoredSession) error {
	if value.Session.ID == "" || value.Session.ContentID == "" {
		return errors.New("invalid automatic subtitle session")
	}
	draft, err := json.Marshal(value.Draft)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO automatic_subtitle_sessions(id,content_id,language,capability,state,processed_seconds,furthest_position,duration_seconds,job_id,error_code,error_message,draft_payload,revision,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET content_id=excluded.content_id,language=excluded.language,capability=excluded.capability,state=excluded.state,processed_seconds=excluded.processed_seconds,furthest_position=excluded.furthest_position,duration_seconds=excluded.duration_seconds,job_id=excluded.job_id,error_code=excluded.error_code,error_message=excluded.error_message,draft_payload=excluded.draft_payload,revision=excluded.revision,updated_at=excluded.updated_at`, value.Session.ID, value.Session.ContentID, value.Session.Language, value.Session.Capability, value.Session.State, value.Session.ProcessedSeconds, value.Session.FurthestPosition, value.Session.DurationSeconds, value.Session.JobID, value.Session.ErrorCode, value.Session.ErrorMessage, string(draft), value.Revision, value.Session.UpdatedAt)
	return err
}
