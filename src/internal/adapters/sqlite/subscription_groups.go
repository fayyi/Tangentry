package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"tangentry/internal/subscriptions"
)

const groupColumns = `id,name,created_at,revision`

func scanGroup(row interface{ Scan(...any) error }) (subscriptions.Group, error) {
	var group subscriptions.Group
	err := row.Scan(&group.ID, &group.Name, &group.CreatedAt, &group.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		err = subscriptions.ErrGroupNotFound
	}
	return group, err
}
func (s *Store) ListSubscriptionGroups(ctx context.Context) ([]subscriptions.Group, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+groupColumns+` FROM subscription_groups ORDER BY created_at,rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := make([]subscriptions.Group, 0)
	for rows.Next() {
		group, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}
func (s *Store) CreateSubscriptionGroup(ctx context.Context, group subscriptions.Group, key string) (subscriptions.Group, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return subscriptions.Group{}, false, err
	}
	defer tx.Rollback()
	old, err := scanGroup(tx.QueryRowContext(ctx, `SELECT `+groupColumns+` FROM subscription_groups WHERE name_key=?`, key))
	if err == nil {
		return old, false, nil
	}
	if !errors.Is(err, subscriptions.ErrGroupNotFound) {
		return subscriptions.Group{}, false, err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_groups`).Scan(&count); err != nil {
		return subscriptions.Group{}, false, err
	}
	if count >= 100 {
		return subscriptions.Group{}, false, subscriptions.ErrGroupLimit
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO subscription_groups(id,name,name_key,created_at,revision) VALUES(?,?,?,?,1)`, group.ID, group.Name, key, group.CreatedAt); err != nil {
		return subscriptions.Group{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return subscriptions.Group{}, false, err
	}
	return group, true, nil
}
func (s *Store) RenameSubscriptionGroup(ctx context.Context, id string, edit subscriptions.GroupEdit, key string) (subscriptions.Group, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return subscriptions.Group{}, err
	}
	defer tx.Rollback()
	group, err := scanGroup(tx.QueryRowContext(ctx, `SELECT `+groupColumns+` FROM subscription_groups WHERE id=?`, id))
	if err != nil {
		return group, err
	}
	if group.Revision != edit.Revision {
		return group, subscriptions.ErrGroupConflict
	}
	var other string
	err = tx.QueryRowContext(ctx, `SELECT id FROM subscription_groups WHERE name_key=? AND id<>?`, key, id).Scan(&other)
	if err == nil {
		return group, subscriptions.ErrGroupName
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return group, err
	}
	if group.Name == edit.Name {
		return group, nil
	}
	group, err = scanGroup(tx.QueryRowContext(ctx, `UPDATE subscription_groups SET name=?,name_key=?,revision=revision+1 WHERE id=? RETURNING `+groupColumns, edit.Name, key, id))
	if err != nil {
		return group, err
	}
	return group, tx.Commit()
}
func (s *Store) DeleteSubscriptionGroup(ctx context.Context, id string, revision int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	group, err := scanGroup(tx.QueryRowContext(ctx, `SELECT `+groupColumns+` FROM subscription_groups WHERE id=?`, id))
	if err != nil {
		return err
	}
	if group.Revision != revision {
		return subscriptions.ErrGroupConflict
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM subscription_group_members WHERE group_id=?`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM subscription_groups WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) AssignSubscriptionGroup(ctx context.Context, sourceID, groupID, expected string) (subscriptions.Membership, error) {
	membership := subscriptions.Membership{SourceID: sourceID, GroupID: groupID}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return membership, err
	}
	defer tx.Rollback()
	var current string
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(m.group_id,'') FROM subscriptions s LEFT JOIN subscription_group_members m ON m.source_id=s.id WHERE s.id=?`, sourceID).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return membership, subscriptions.ErrNotFound
	}
	if err != nil {
		return membership, err
	}
	if current != expected {
		return membership, subscriptions.ErrGroupConflict
	}
	if groupID != "" {
		if _, err = scanGroup(tx.QueryRowContext(ctx, `SELECT `+groupColumns+` FROM subscription_groups WHERE id=?`, groupID)); err != nil {
			return membership, err
		}
	}
	if current == groupID {
		return membership, nil
	}
	if groupID == "" {
		_, err = tx.ExecContext(ctx, `DELETE FROM subscription_group_members WHERE source_id=?`, sourceID)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO subscription_group_members(source_id,group_id) VALUES(?,?) ON CONFLICT(source_id) DO UPDATE SET group_id=excluded.group_id`, sourceID, groupID)
	}
	if err != nil {
		return membership, err
	}
	return membership, tx.Commit()
}
