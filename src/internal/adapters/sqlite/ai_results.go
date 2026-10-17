package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"tangentry/internal/ai"
)

func (s *Store) AIResult(ctx context.Context, kind, sourceHash, target string) (ai.StoredResult, error) {
	row := ai.StoredResult{Kind: kind, SourceHash: sourceHash, Target: target}
	err := s.db.QueryRowContext(ctx, `SELECT model,value,created_at FROM ai_result WHERE kind=? AND source_hash=? AND target=?`, kind, sourceHash, target).Scan(&row.Model, &row.Value, &row.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ai.StoredResult{}, ai.ErrNotFound
	}
	return row, err
}

func (s *Store) AIResults(ctx context.Context, kind, target string) ([]ai.StoredResult, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT source_hash,model,value,created_at FROM ai_result WHERE kind=? AND target=? ORDER BY created_at DESC`, kind, target)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ai.StoredResult{}
	for rows.Next() {
		row := ai.StoredResult{Kind: kind, Target: target}
		if err := rows.Scan(&row.SourceHash, &row.Model, &row.Value, &row.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) PutAIResult(ctx context.Context, row ai.StoredResult) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO ai_result(kind,source_hash,target,model,value,created_at) VALUES(?,?,?,?,?,?)
	 ON CONFLICT(kind,source_hash,target) DO UPDATE SET model=excluded.model, value=excluded.value`,
		row.Kind, row.SourceHash, row.Target, row.Model, row.Value, row.CreatedAt)
	return err
}

func (s *Store) DeleteAIResult(ctx context.Context, kind, sourceHash, target string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM ai_result WHERE kind=? AND source_hash=? AND target=?`, kind, sourceHash, target)
	return err
}
