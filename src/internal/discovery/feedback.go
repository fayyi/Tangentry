package discovery

import (
	"context"
	"errors"
	"strings"
	"time"

	"tangentry/internal/content"
)

var ErrInvalidFeedback = errors.New("invalid discovery feedback")

type FeedbackType string

const (
	FeedbackMore  FeedbackType = "more"
	FeedbackLess  FeedbackType = "less"
	FeedbackHide  FeedbackType = "hide"
	FeedbackBlock FeedbackType = "block_creator"
)

type FeedbackTarget string

const (
	TargetItem    FeedbackTarget = "item"
	TargetSource  FeedbackTarget = "source"
	TargetCreator FeedbackTarget = "creator"
	TargetTopic   FeedbackTarget = "topic"
)

type Feedback struct {
	ID        string         `json:"id"`
	Type      FeedbackType   `json:"type"`
	Target    FeedbackTarget `json:"target"`
	Value     string         `json:"value"`
	ExpiresAt int64          `json:"expiresAt,omitempty"`
	CreatedAt int64          `json:"createdAt"`
}

type FeedbackInput struct {
	Type   FeedbackType   `json:"type"`
	Target FeedbackTarget `json:"target"`
	Value  string         `json:"value"`
}

type FeedbackRepository interface {
	ListDiscoveryFeedback(context.Context, int64) ([]Feedback, error)
	SaveDiscoveryFeedback(context.Context, Feedback) error
	DeleteDiscoveryFeedback(context.Context, string) error
	ClearDiscoveryFeedback(context.Context) error
}

type FeedbackService struct {
	repository FeedbackRepository
	clock      func() time.Time
}

func NewFeedbackService(repository FeedbackRepository) *FeedbackService {
	return &FeedbackService{repository: repository, clock: time.Now}
}

func (s *FeedbackService) List(ctx context.Context) ([]Feedback, error) {
	return s.repository.ListDiscoveryFeedback(ctx, s.clock().UnixMilli())
}

func (s *FeedbackService) Save(ctx context.Context, input FeedbackInput) (Feedback, error) {
	value := strings.TrimSpace(input.Value)
	if !validFeedback(input.Type, input.Target, value) {
		return Feedback{}, ErrInvalidFeedback
	}
	now := s.clock().UnixMilli()
	expires := int64(0)
	if input.Type == FeedbackLess {
		expires = s.clock().Add(7 * 24 * time.Hour).UnixMilli()
	}
	feedback := Feedback{ID: feedbackID(input.Type, input.Target, value), Type: input.Type, Target: input.Target, Value: value, ExpiresAt: expires, CreatedAt: now}
	if err := s.repository.SaveDiscoveryFeedback(ctx, feedback); err != nil {
		return Feedback{}, err
	}
	return feedback, nil
}

func (s *FeedbackService) Delete(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return ErrInvalidFeedback
	}
	return s.repository.DeleteDiscoveryFeedback(ctx, id)
}

func (s *FeedbackService) Clear(ctx context.Context) error {
	return s.repository.ClearDiscoveryFeedback(ctx)
}

func validFeedback(kind FeedbackType, target FeedbackTarget, value string) bool {
	if value == "" || len(value) > 300 || strings.ContainsAny(value, "\x00\r\n") {
		return false
	}
	switch kind {
	case FeedbackMore, FeedbackLess:
		return target == TargetSource || target == TargetCreator || target == TargetTopic
	case FeedbackHide:
		return target == TargetItem && content.ValidID(value)
	case FeedbackBlock:
		return target == TargetCreator
	default:
		return false
	}
}

func feedbackID(kind FeedbackType, target FeedbackTarget, value string) string {
	if kind == FeedbackMore || kind == FeedbackLess {
		// These are mutually exclusive preferences for the same target. Reusing
		// one key makes the latest choice replace the previous one.
		return "preference:" + string(target) + ":" + value
	}
	return string(kind) + ":" + string(target) + ":" + value
}

// Matches reports whether feedback applies to a candidate. Expired feedback
// is ignored by the caller's active list query.
func (f Feedback) Matches(item content.Item) bool {
	if f.Target == TargetItem {
		return item.ID == f.Value
	}
	if f.Target == TargetSource {
		return item.Platform == f.Value
	}
	if f.Target == TargetTopic {
		return strings.EqualFold(strings.TrimSpace(item.Category), f.Value)
	}
	creator := item.AuthorID
	if creator == "" {
		creator = item.Author
	}
	return creator == f.Value
}
