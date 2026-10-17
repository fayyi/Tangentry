// Package readingprogress owns a content's last playback position, independent
// of library membership, platform availability and downloaded file identity.
package readingprogress

import (
	"context"
	"errors"
	"math"
	"regexp"
	"time"
)

var ErrInvalid = errors.New("invalid reading progress")
var ErrConflict = errors.New("reading progress changed")
var contentID = regexp.MustCompile(`^(bilibili:BV[0-9A-Za-z]{10}|youtube:[A-Za-z0-9_-]{11}|xiaohongshu:[a-f0-9]{24}|xiaoyuzhou:[a-f0-9]{24}|douyin:[1-9][0-9]{15,19}|rss:[a-f0-9]{64})$`)

type Progress struct {
	ContentID       string  `json:"contentId"`
	PositionSeconds float64 `json:"positionSeconds"`
	DurationSeconds float64 `json:"durationSeconds"`
	Completed       bool    `json:"completed"`
	Revision        int64   `json:"revision"`
	UpdatedAt       int64   `json:"updatedAt"`
}
type Input struct {
	PositionSeconds float64
	DurationSeconds float64
	Revision        int64
}
type Repository interface {
	GetReadingProgress(context.Context, string) (*Progress, error)
	ListReadingProgress(context.Context, []string) ([]Progress, error)
	SaveReadingProgress(context.Context, Progress, int64) (Progress, error)
}
type Service struct{ repository Repository }

func NewService(repository Repository) *Service { return &Service{repository: repository} }
func (s *Service) Get(ctx context.Context, id string) (*Progress, error) {
	if !contentID.MatchString(id) {
		return nil, ErrInvalid
	}
	return s.repository.GetReadingProgress(ctx, id)
}

// Batch reads only requested identities; missing rows stay missing and never
// create progress. A bounded query avoids one request and SQL read per card.
func (s *Service) Batch(ctx context.Context, ids []string) ([]Progress, error) {
	if len(ids) == 0 || len(ids) > 100 {
		return nil, ErrInvalid
	}
	unique := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !contentID.MatchString(id) {
			return nil, ErrInvalid
		}
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	return s.repository.ListReadingProgress(ctx, unique)
}
func (s *Service) Save(ctx context.Context, id string, input Input) (Progress, error) {
	if !contentID.MatchString(id) || input.Revision < 0 || input.Revision >= 9007199254740991 || math.IsNaN(input.PositionSeconds) || math.IsInf(input.PositionSeconds, 0) || math.IsNaN(input.DurationSeconds) || math.IsInf(input.DurationSeconds, 0) || input.DurationSeconds <= 0 || input.DurationSeconds > 7*24*3600 || input.PositionSeconds < 0 || input.PositionSeconds > input.DurationSeconds {
		return Progress{}, ErrInvalid
	}
	// Millisecond precision is enough for resuming, and avoids floating point
	// noise triggering repeated database writes for the same paused position.
	duration := math.Round(input.DurationSeconds*1000) / 1000
	position := math.Round(input.PositionSeconds*1000) / 1000
	if duration <= 0 {
		return Progress{}, ErrInvalid
	}
	value := Progress{ContentID: id, PositionSeconds: position, DurationSeconds: duration, Completed: position == duration, Revision: input.Revision + 1, UpdatedAt: time.Now().UnixMilli()}
	return s.repository.SaveReadingProgress(ctx, value, input.Revision)
}
