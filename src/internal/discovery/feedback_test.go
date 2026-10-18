package discovery

import (
	"context"
	"testing"
	"time"

	"tangentry/internal/content"
)

type feedbackRepo struct{ values map[string]Feedback }

func (r *feedbackRepo) ListDiscoveryFeedback(_ context.Context, now int64) ([]Feedback, error) {
	result := make([]Feedback, 0, len(r.values))
	for _, value := range r.values {
		if value.ExpiresAt == 0 || value.ExpiresAt > now {
			result = append(result, value)
		}
	}
	return result, nil
}
func (r *feedbackRepo) SaveDiscoveryFeedback(_ context.Context, value Feedback) error {
	if r.values == nil {
		r.values = map[string]Feedback{}
	}
	r.values[value.ID] = value
	return nil
}
func (r *feedbackRepo) DeleteDiscoveryFeedback(_ context.Context, id string) error {
	delete(r.values, id)
	return nil
}
func (r *feedbackRepo) ClearDiscoveryFeedback(_ context.Context) error {
	r.values = map[string]Feedback{}
	return nil
}

func TestFeedbackExpiresAndCanBeRevoked(t *testing.T) {
	repo := &feedbackRepo{}
	service := NewFeedbackService(repo)
	service.clock = func() time.Time { return time.UnixMilli(1000) }
	value, err := service.Save(context.Background(), FeedbackInput{Type: FeedbackLess, Target: TargetCreator, Value: "creator-1"})
	if err != nil || value.ExpiresAt != 7*24*60*60*1000+1000 {
		t.Fatalf("feedback expiry: %#v %v", value, err)
	}
	if values, err := service.List(context.Background()); err != nil || len(values) != 1 {
		t.Fatalf("feedback list: %#v %v", values, err)
	}
	if err := service.Delete(context.Background(), value.ID); err != nil {
		t.Fatal(err)
	}
	if values, _ := service.List(context.Background()); len(values) != 0 {
		t.Fatal("feedback was not revoked")
	}
}

func TestFeedbackMatchesTargets(t *testing.T) {
	item := content.Item{ID: "youtube:abc", Platform: "youtube", AuthorID: "creator-1", Author: "创作者", Category: "游戏"}
	for _, value := range []Feedback{
		{Type: FeedbackMore, Target: TargetSource, Value: "youtube"},
		{Type: FeedbackMore, Target: TargetCreator, Value: "creator-1"},
		{Type: FeedbackMore, Target: TargetTopic, Value: "游戏"},
		{Type: FeedbackHide, Target: TargetItem, Value: item.ID},
	} {
		if !value.Matches(item) {
			t.Fatalf("feedback did not match: %#v", value)
		}
	}
}
