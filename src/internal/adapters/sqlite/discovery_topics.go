package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"tangentry/internal/discovery"
)

const discoveryTopicColumns = `id,name,keywords,group_id,revision,created_at`

func scanDiscoveryTopic(row interface{ Scan(...any) error }) (discovery.TopicRule, error) {
	var topic discovery.TopicRule
	var raw string
	err := row.Scan(&topic.ID, &topic.Name, &raw, &topic.GroupID, &topic.Revision, &topic.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return topic, discovery.ErrTopicNotFound
	}
	if err != nil {
		return topic, err
	}
	if json.Unmarshal([]byte(raw), &topic.Keywords) != nil {
		return topic, discovery.ErrTopicInvalid
	}
	return topic, nil
}

func (s *Store) ListDiscoveryTopics(ctx context.Context) ([]discovery.TopicRule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+discoveryTopicColumns+` FROM discovery_topic_rules ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]discovery.TopicRule, 0)
	for rows.Next() {
		value, err := scanDiscoveryTopic(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (s *Store) CreateDiscoveryTopic(ctx context.Context, topic discovery.TopicRule) (discovery.TopicRule, error) {
	raw, err := json.Marshal(topic.Keywords)
	if err != nil {
		return discovery.TopicRule{}, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO discovery_topic_rules(id,name,keywords,group_id,revision,created_at) VALUES(?,?,?,?,1,strftime('%s','now'))`, topic.ID, topic.Name, string(raw), topic.GroupID)
	if err != nil {
		return discovery.TopicRule{}, err
	}
	return s.GetDiscoveryTopic(ctx, topic.ID)
}

func (s *Store) GetDiscoveryTopic(ctx context.Context, id string) (discovery.TopicRule, error) {
	return scanDiscoveryTopic(s.db.QueryRowContext(ctx, `SELECT `+discoveryTopicColumns+` FROM discovery_topic_rules WHERE id=?`, id))
}

func (s *Store) UpdateDiscoveryTopic(ctx context.Context, id string, edit discovery.TopicRuleEdit) (discovery.TopicRule, error) {
	raw, err := json.Marshal(edit.Keywords)
	if err != nil {
		return discovery.TopicRule{}, err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE discovery_topic_rules SET name=?,keywords=?,group_id=?,revision=revision+1 WHERE id=? AND revision=?`, edit.Name, string(raw), edit.GroupID, id, edit.Revision)
	if err != nil {
		return discovery.TopicRule{}, err
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		if _, getErr := s.GetDiscoveryTopic(ctx, id); errors.Is(getErr, discovery.ErrTopicNotFound) {
			return discovery.TopicRule{}, getErr
		}
		return discovery.TopicRule{}, discovery.ErrTopicConflict
	}
	return s.GetDiscoveryTopic(ctx, id)
}

func (s *Store) DeleteDiscoveryTopic(ctx context.Context, id string, revision int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM discovery_topic_rules WHERE id=? AND revision=?`, id, revision)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		if _, getErr := s.GetDiscoveryTopic(ctx, id); errors.Is(getErr, discovery.ErrTopicNotFound) {
			return getErr
		}
		return discovery.ErrTopicConflict
	}
	return nil
}
