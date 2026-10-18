package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"tangentry/internal/subscriptions"
)

const savedViewColumns = `id,name,definition_version,definition,icon,color,sort_order,is_default,builtin`

func scanSavedView(row interface{ Scan(...any) error }) (subscriptions.SavedView, error) {
	var view subscriptions.SavedView
	var raw string
	err := row.Scan(&view.ID, &view.Name, &view.DefinitionVersion, &raw, &view.Icon, &view.Color, &view.SortOrder, &view.Default, &view.Builtin)
	if errors.Is(err, sql.ErrNoRows) {
		return view, subscriptions.ErrSavedViewNotFound
	}
	if err != nil {
		return view, err
	}
	if err = json.Unmarshal([]byte(raw), &view.Definition); err != nil {
		return view, subscriptions.ErrSavedViewInvalid
	}
	return view, nil
}

func (s *Store) ListSavedViews(ctx context.Context) ([]subscriptions.SavedView, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+savedViewColumns+` FROM saved_views ORDER BY sort_order,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	views := make([]subscriptions.SavedView, 0)
	for rows.Next() {
		view, err := scanSavedView(rows)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, rows.Err()
}

func (s *Store) GetSavedView(ctx context.Context, id string) (subscriptions.SavedView, error) {
	return scanSavedView(s.db.QueryRowContext(ctx, `SELECT `+savedViewColumns+` FROM saved_views WHERE id=?`, id))
}

func (s *Store) CreateSavedView(ctx context.Context, view subscriptions.SavedView) (subscriptions.SavedView, error) {
	raw, err := json.Marshal(view.Definition)
	if err != nil {
		return subscriptions.SavedView{}, err
	}
	var sortOrder int
	if err = s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order),-1)+1 FROM saved_views`).Scan(&sortOrder); err != nil {
		return subscriptions.SavedView{}, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO saved_views(id,name,definition_version,definition,icon,color,sort_order,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, view.ID, view.Name, view.DefinitionVersion, string(raw), view.Icon, view.Color, sortOrder, time.Now().Unix(), time.Now().Unix())
	if err != nil {
		return subscriptions.SavedView{}, err
	}
	return s.GetSavedView(ctx, view.ID)
}

func (s *Store) UpdateSavedView(ctx context.Context, id string, edit subscriptions.SavedViewEdit) (subscriptions.SavedView, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return subscriptions.SavedView{}, err
	}
	defer tx.Rollback()
	view, err := scanSavedView(tx.QueryRowContext(ctx, `SELECT `+savedViewColumns+` FROM saved_views WHERE id=?`, id))
	if err != nil {
		return view, err
	}
	if edit.Name != nil {
		view.Name = *edit.Name
	}
	if edit.Definition != nil {
		view.Definition = *edit.Definition
		view.DefinitionVersion = edit.Definition.Version
	}
	if edit.Icon != nil {
		view.Icon = *edit.Icon
	}
	if edit.Color != nil {
		view.Color = *edit.Color
	}
	if edit.SortOrder != nil {
		view.SortOrder = *edit.SortOrder
	}
	if edit.Default != nil {
		view.Default = *edit.Default
	}
	raw, err := json.Marshal(view.Definition)
	if err != nil {
		return view, err
	}
	if view.Default {
		if _, err = tx.ExecContext(ctx, `UPDATE saved_views SET is_default=0 WHERE is_default=1`); err != nil {
			return view, err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE saved_views SET name=?,definition_version=?,definition=?,icon=?,color=?,sort_order=?,is_default=?,updated_at=? WHERE id=?`, view.Name, view.DefinitionVersion, string(raw), view.Icon, view.Color, view.SortOrder, view.Default, time.Now().Unix(), id)
	if err != nil {
		return view, err
	}
	if err = tx.Commit(); err != nil {
		return view, err
	}
	return s.GetSavedView(ctx, id)
}

func (s *Store) DeleteSavedView(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM saved_views WHERE id=?`, id)
	if err != nil {
		return err
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if deleted == 0 {
		return subscriptions.ErrSavedViewNotFound
	}
	return nil
}

func (s *Store) ReorderSavedViews(ctx context.Context, ids []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var total int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM saved_views`).Scan(&total); err != nil {
		return err
	}
	if total != len(ids) {
		return subscriptions.ErrSavedViewInvalid
	}
	for order, id := range ids {
		result, err := tx.ExecContext(ctx, `UPDATE saved_views SET sort_order=?,updated_at=? WHERE id=?`, order, time.Now().Unix(), id)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count == 0 {
			return subscriptions.ErrSavedViewNotFound
		}
	}
	return tx.Commit()
}
