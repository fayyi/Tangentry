package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"tangentry/internal/readstate"
)

type stateQuery interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func listReadStates(ctx context.Context, query stateQuery, ids []string) ([]readstate.State, error) {
	result := make([]readstate.State, 0, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := query.QueryContext(ctx, `SELECT content_id, is_read, updated_at FROM read_state WHERE content_id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	known := map[string]readstate.State{}
	for rows.Next() {
		var state readstate.State
		if err := rows.Scan(&state.ContentID, &state.Read, &state.UpdatedAt); err != nil {
			return nil, err
		}
		known[state.ContentID] = state
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		if state, ok := known[id]; ok {
			result = append(result, state)
		} else {
			result = append(result, readstate.State{ContentID: id})
		}
	}
	return result, nil
}

func (s *Store) ListReadStates(ctx context.Context, ids []string) ([]readstate.State, error) {
	return listReadStates(ctx, s.db, ids)
}

func (s *Store) SetReadStates(ctx context.Context, ids []string, read bool, at int64) ([]readstate.State, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if read {
			_, err = tx.ExecContext(ctx, `INSERT INTO read_state(content_id,is_read,updated_at) VALUES(?,1,?) ON CONFLICT(content_id) DO UPDATE SET is_read=1,updated_at=excluded.updated_at WHERE read_state.is_read=0`, id, at)
		} else {
			// Unseen entries are already unread: do not create passive rows.
			_, err = tx.ExecContext(ctx, `UPDATE read_state SET is_read=0,updated_at=? WHERE content_id=? AND is_read=1`, at, id)
		}
		if err != nil {
			return nil, err
		}
	}
	states, err := listReadStates(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return states, nil
}
