// Package readstate owns whether content has been opened for reading. It is
// independent of subscription snapshots, bookmarks and media playback progress.
package readstate

import (
	"context"
	"errors"
	"tangentry/internal/content"
	"time"
)

var ErrInvalid = errors.New("invalid read state")

const MaxBatch = 200

type State struct {
	ContentID string `json:"contentId"`
	Read      bool   `json:"read"`
	UpdatedAt int64  `json:"updatedAt"`
}

type Repository interface {
	ListReadStates(context.Context, []string) ([]State, error)
	SetReadStates(context.Context, []string, bool, int64) ([]State, error)
}

type Service struct{ repository Repository }

func New(repository Repository) *Service { return &Service{repository: repository} }

func identities(ids []string) ([]string, error) {
	if len(ids) == 0 || len(ids) > MaxBatch {
		return nil, ErrInvalid
	}
	result := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if !content.ValidID(id) {
			return nil, ErrInvalid
		}
		if !seen[id] {
			result = append(result, id)
			seen[id] = true
		}
	}
	return result, nil
}

func (s *Service) Batch(ctx context.Context, ids []string) ([]State, error) {
	ids, err := identities(ids)
	if err != nil {
		return nil, err
	}
	return s.repository.ListReadStates(ctx, ids)
}

// Set applies an explicit intent to these identities only. A simultaneous feed
// refresh cannot add unseen entries to a user's bulk mark action.
func (s *Service) Set(ctx context.Context, ids []string, read bool) ([]State, error) {
	ids, err := identities(ids)
	if err != nil {
		return nil, err
	}
	return s.repository.SetReadStates(ctx, ids, read, time.Now().UnixMilli())
}
