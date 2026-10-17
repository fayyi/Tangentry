package subscriptions

import (
	"context"
	"strings"
	"tangentry/internal/discovery"
	"time"
	"unicode/utf8"
)

type ChannelResolver func(context.Context, string) (discovery.YouTubeChannel, error)

func WithYouTubeResolver(resolve ChannelResolver) Option {
	return func(s *Service) { s.resolveChannel = resolve }
}

func (s *Service) ResolveYouTube(ctx context.Context, raw string) (discovery.YouTubeChannel, error) {
	reference, err := discovery.ParseYouTubeChannel(raw)
	if err != nil {
		return discovery.YouTubeChannel{}, ErrChannelInput
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return discovery.YouTubeChannel{}, context.Canceled
	}
	s.wg.Add(1)
	s.mu.Unlock()
	defer s.wg.Done()
	if reference.ID != "" {
		return discovery.YouTubeChannel{ID: reference.ID, URL: reference.URL}, nil
	}
	if s.resolveChannel == nil {
		return discovery.YouTubeChannel{}, &discovery.PlatformError{Message: "频道名称解析暂不可用，请输入以 UC 开头的频道 ID。"}
	}
	select {
	case s.updates <- struct{}{}:
		defer func() { <-s.updates }()
	default:
		return discovery.YouTubeChannel{}, ErrBusy
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	stop := context.AfterFunc(s.ctx, cancel)
	defer func() { stop(); cancel() }()
	channel, err := s.resolveChannel(ctx, reference.URL)
	if err != nil {
		return discovery.YouTubeChannel{}, err
	}
	if ctx.Err() != nil {
		return discovery.YouTubeChannel{}, ctx.Err()
	}
	if !discovery.YouTubeChannelID(channel.ID) || !utf8.ValidString(channel.Title) || utf8.RuneCountInString(channel.Title) > 200 || strings.ContainsAny(channel.Title, "\x00\r\n") {
		return discovery.YouTubeChannel{}, ErrSnapshot
	}
	if channel.URL != "" {
		confirmed, err := discovery.ParseYouTubeChannel(channel.URL)
		if err != nil || confirmed.ID != channel.ID {
			return discovery.YouTubeChannel{}, ErrSnapshot
		}
	}
	channel.URL = discovery.YouTubeChannelURL(channel.ID)
	return channel, nil
}
