package discovery

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"

	"tangentry/internal/content"
)

type QueueCursor struct {
	Account   string `json:"account"`
	Snapshot  int64  `json:"snapshot"`
	Seen      bool   `json:"seen"`
	FetchedAt int64  `json:"fetchedAt"`
	ID        string `json:"id"`
}

type QueuePage struct {
	Items []QueuedItem `json:"items"`
	Next  string       `json:"next"`
}

type QueuePageRepository interface {
	PageQueuedRecommendations(context.Context, QueueScope, QueueCursor, int) ([]QueuedItem, error)
}

func (q *RecommendationQueue) PendingPage(ctx context.Context, platform string, limit int, token string) (QueuePage, error) {
	scope := q.scope(platform)
	if !ValidQueueScope(scope) || scope.Platform != platform || limit < 1 || limit > 500 || len(token) > 1024 {
		return QueuePage{}, ErrInvalidQuery
	}
	cursor := QueueCursor{Account: scope.Account, Snapshot: q.clock().UnixMilli()}
	if token != "" {
		cursor = QueueCursor{}
		raw, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil || json.Unmarshal(raw, &cursor) != nil || cursor.Account != scope.Account || cursor.Snapshot <= 0 || cursor.Snapshot > q.clock().UnixMilli() || cursor.FetchedAt <= 0 || cursor.FetchedAt > cursor.Snapshot || !content.ValidID(cursor.ID) || !strings.HasPrefix(cursor.ID, platform+":") {
			return QueuePage{}, ErrInvalidQuery
		}
	}
	repository, ok := q.repository.(QueuePageRepository)
	if !ok {
		return QueuePage{}, ErrUnavailable
	}
	items, err := repository.PageQueuedRecommendations(ctx, scope, cursor, limit+1)
	if err != nil {
		return QueuePage{}, err
	}
	if q.scope(platform) != scope {
		return QueuePage{}, ErrUnavailable
	}
	page := QueuePage{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		last := page.Items[limit-1]
		cursor.FetchedAt, cursor.ID = last.FetchedAt, last.Item.ID
		cursor.Seen = last.SeenAt > 0 && last.SeenAt <= cursor.Snapshot
		raw, err := json.Marshal(cursor)
		if err != nil {
			return QueuePage{}, err
		}
		page.Next = base64.RawURLEncoding.EncodeToString(raw)
	}
	return page, nil
}
