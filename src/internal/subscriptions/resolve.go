package subscriptions

import (
	"context"
	"net/url"
	"strings"
	"time"

	"tangentry/internal/discovery"
	"tangentry/internal/papers"
)

type ResolvedSource struct {
	Type  string `json:"type"`
	Query string `json:"query"`
	Title string `json:"title"`
	URL   string `json:"url"`
}
type SourceResolver func(context.Context, string, string) (string, error)

func WithSourceResolver(resolve SourceResolver) Option {
	return func(s *Service) { s.resolveSource = resolve }
}

func (s *Service) Resolve(ctx context.Context, raw string) (ResolvedSource, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 2048 {
		return ResolvedSource{}, ErrInvalid
	}
	result := ResolvedSource{}
	if strings.HasPrefix(strings.ToLower(raw), "arxiv:") {
		category, err := papers.NormalizeCategory(strings.TrimSpace(raw[len("arxiv:"):]))
		if err != nil {
			return result, ErrInvalid
		}
		result = ResolvedSource{Type: "arxiv", Query: category, Title: "arXiv 分类 " + category, URL: "https://rss.arxiv.org/rss/" + category}
	} else if strings.HasPrefix(strings.ToLower(raw), "huggingface:") {
		date := strings.TrimSpace(raw[len("huggingface:"):])
		if date == "" {
			date = "today"
		}
		if _, err := paperDate(date); err != nil {
			return result, ErrInvalid
		}
		result = ResolvedSource{Type: "huggingface", Query: date, Title: "HF 每日论文", URL: "https://huggingface.co/papers"}
	} else if _, err := discovery.ParseYouTubeChannel(raw); err == nil {
		channel, err := s.ResolveYouTube(ctx, raw)
		if err != nil {
			return result, err
		}
		result = ResolvedSource{Type: "youtube", Query: channel.ID, Title: channel.Title, URL: channel.URL}
	} else if kind, id, err := discovery.ParseQuery(raw); err == nil && kind == "creator" {
		result = ResolvedSource{Type: "bilibili", Query: id, URL: SourceURL("bilibili:creator:"+id, id)}
	} else {
		u, err := url.Parse(raw)
		if err != nil {
			return result, ErrInvalid
		}
		host := strings.ToLower(u.Hostname())
		for _, domain := range []string{"youtube.com", "youtu.be", "bilibili.com", "b23.tv", "xiaoyuzhoufm.com", "xiaohongshu.com", "xhslink.com", "douyin.com", "iesdouyin.com", "tiktok.com", "twitter.com", "x.com"} {
			if host == domain || strings.HasSuffix(host, "."+domain) {
				return result, &discovery.PlatformError{Message: "这个地址不是可订阅的频道或 RSS，请使用创作者频道地址或 RSS 订阅地址。"}
			}
		}
		address, err := rssAddress(raw)
		if err != nil {
			return result, ErrInvalid
		}
		result = ResolvedSource{Type: "rss", Query: address, URL: address}
	}
	if result.Title == "" {
		if s.resolveSource == nil {
			return ResolvedSource{}, &discovery.PlatformError{Message: "订阅名称暂时无法读取，请稍后重试。"}
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return ResolvedSource{}, context.Canceled
		}
		s.wg.Add(1)
		s.mu.Unlock()
		defer s.wg.Done()
		select {
		case s.updates <- struct{}{}:
			defer func() { <-s.updates }()
		default:
			return ResolvedSource{}, ErrBusy
		}
		ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
		stop := context.AfterFunc(s.ctx, cancel)
		defer func() { stop(); cancel() }()
		title, err := s.resolveSource(ctx, result.Type, result.Query)
		if err != nil {
			// A reachable feed is helpful for naming, but it must not prevent
			// saving a valid private or temporarily unavailable RSS source.
			if result.Type == "rss" {
				result.Title = "RSS 订阅"
				return result, nil
			}
			if result.Type == "bilibili" {
				result.Title = "B站 UP 主"
				return result, nil
			}
			if result.Type == "youtube" && discovery.YouTubeChannelID(result.Query) {
				result.Title = "YouTube 频道"
				return result, nil
			}
			return ResolvedSource{}, err
		}
		title = strings.TrimSpace(title)
		if title == "" || strings.ContainsAny(title, "\x00\r\n") {
			return ResolvedSource{}, ErrSnapshot
		}
		runes := []rune(title)
		if len(runes) > 100 {
			runes = runes[:100]
		}
		result.Title = string(runes)
	}
	if _, err := name(result.Title); err != nil {
		return ResolvedSource{}, ErrSnapshot
	}
	return result, nil
}

func paperDate(raw string) (time.Time, error) {
	if raw == "today" {
		return time.Now(), nil
	}
	return time.Parse("2006-01-02", raw)
}
