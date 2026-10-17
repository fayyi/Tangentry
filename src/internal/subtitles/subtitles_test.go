package subtitles

import (
	"context"
	"errors"
	"testing"
)

type providerFunc func(context.Context, string) (Document, error)

func (f providerFunc) ReadSubtitles(ctx context.Context, id string) (Document, error) {
	return f(ctx, id)
}

func TestRequestsAreExplicitCanonicalAndBounded(t *testing.T) {
	started := make(chan string, 2)
	s := NewService(providerFunc(func(ctx context.Context, id string) (Document, error) {
		started <- id
		<-ctx.Done()
		return Document{}, ctx.Err()
	}))
	defer s.Close()
	for _, query := range []string{"12345", "https://example.com/BV17x411w7KC", "--exec rm", ""} {
		if _, err := s.Read(context.Background(), query); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted invalid input %q", query)
		}
	}
	if len(started) != 0 {
		t.Fatal("invalid input triggered platform request")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := s.Read(ctx, "https://www.bilibili.com/video/BV17x411w7KC/?untrusted=ignored")
			done <- err
		}()
		if id := <-started; id != "BV17x411w7KC" {
			t.Fatalf("did not canonicalize video: %q", id)
		}
	}
	if _, err := s.Read(context.Background(), "BV17x411w7KC"); !errors.Is(err, ErrBusy) {
		t.Fatal("unbounded processes allowed")
	}
	cancel()
	for range 2 {
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation not propagated")
		}
	}
	if len(s.slots) != 0 {
		t.Fatal("cancelled readers leaked slots")
	}
}

func TestCloseCancelsActiveReadsAndRejectsNewWork(t *testing.T) {
	started := make(chan struct{})
	s := NewService(providerFunc(func(ctx context.Context, _ string) (Document, error) {
		close(started)
		<-ctx.Done()
		return Document{}, ctx.Err()
	}))
	done := make(chan error, 1)
	go func() { _, err := s.Read(context.Background(), "BV17x411w7KC"); done <- err }()
	<-started
	s.Close()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown did not cancel read: %v", err)
	}
	if _, err := s.Read(context.Background(), "BV17x411w7KC"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("new work accepted during shutdown")
	}
	s.Close()
}

type mixedProvider struct {
	providerFunc
	youtube func(context.Context, string, string) (Document, error)
}

func (p mixedProvider) ReadYouTubeSubtitles(ctx context.Context, id, language string) (Document, error) {
	return p.youtube(ctx, id, language)
}

func TestYouTubeSharesReadLimitsAndCanonicalizesSelection(t *testing.T) {
	entered := make(chan string, 2)
	provider := mixedProvider{providerFunc: func(ctx context.Context, id string) (Document, error) {
		entered <- id
		<-ctx.Done()
		return Document{}, ctx.Err()
	}, youtube: func(ctx context.Context, id, language string) (Document, error) {
		entered <- id + ":" + language
		<-ctx.Done()
		return Document{}, ctx.Err()
	}}
	service := NewService(provider)
	defer service.Close()
	for _, input := range []struct{ query, language string }{{"not-a-url", ""}, {"https://example.org/watch?v=jNQXAC9IVRw", "en"}, {"https://youtu.be/jNQXAC9IVRw", "../../file"}} {
		if _, err := service.ReadYouTube(context.Background(), input.query, input.language); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid source or language reached provider")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 2)
	go func() { _, err := service.Read(ctx, "BV17x411w7KC"); done <- err }()
	<-entered
	go func() {
		_, err := service.ReadYouTube(ctx, "https://youtu.be/jNQXAC9IVRw?t=12", "zh-Hans")
		done <- err
	}()
	if got := <-entered; got != "jNQXAC9IVRw:zh-Hans" {
		t.Fatal("YouTube query not canonicalized", got)
	}
	if _, err := service.ReadYouTube(context.Background(), "https://youtu.be/jNQXAC9IVRw", "en"); !errors.Is(err, ErrBusy) {
		t.Fatal("YouTube bypassed shared read limit")
	}
	cancel()
	for range 2 {
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal("caption cancellation lost")
		}
	}
	if len(service.slots) != 0 {
		t.Fatal("cancelled source retained slot")
	}
	service.Close()
	if _, err := service.ReadYouTube(context.Background(), "https://youtu.be/jNQXAC9IVRw", ""); !errors.Is(err, ErrUnavailable) {
		t.Fatal("source read after close")
	}
}
