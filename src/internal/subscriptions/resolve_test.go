package subscriptions_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/discovery"
	"tangentry/internal/subscriptions"
)

func TestConfirmedChannelIDSurvivesOptionalNameFailure(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := subscriptions.NewService(store, nil, subscriptions.WithSourceResolver(func(context.Context, string, string) (string, error) {
		return "", errors.New("name unavailable")
	}), subscriptions.WithYouTubeResolver(func(context.Context, string) (discovery.YouTubeChannel, error) {
		return discovery.YouTubeChannel{}, errors.New("handle unavailable")
	}))
	defer service.Close()
	resolved, err := service.Resolve(context.Background(), channelID)
	if err != nil || resolved.Query != channelID || resolved.Type != "youtube" || resolved.Title != "YouTube 频道" {
		t.Fatal(resolved, err)
	}
	if _, err := service.Resolve(context.Background(), "@unknown"); err == nil {
		t.Fatal("unconfirmed handle accepted")
	}
}
