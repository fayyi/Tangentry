package subscriptions

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"tangentry/internal/discovery"
	"time"
)

const DefaultInterval = int64(3600)

// Public state contains no feed validators or network credentials.
type RefreshState struct {
	Enabled         bool   `json:"enabled"`
	IntervalSeconds int64  `json:"intervalSeconds"`
	LastAttemptAt   int64  `json:"lastAttemptAt"`
	LastCheckedAt   int64  `json:"lastCheckedAt"`
	NextCheckAt     int64  `json:"nextCheckAt"`
	Failures        int    `json:"failures"`
	Message         string `json:"message,omitempty"`
	ContentRevision int64  `json:"contentRevision"`
}
type Validators struct {
	ETag         string
	LastModified string
	ResourceURL  string
}
type RefreshRecord struct {
	State      RefreshState
	Validators Validators
}

func DefaultRefresh() RefreshRecord {
	return RefreshRecord{State: RefreshState{IntervalSeconds: DefaultInterval}}
}

type ConditionalResult struct {
	Snapshot    Snapshot
	Validators  Validators
	NotModified bool
}
type ConditionalFetcher func(context.Context, string, Validators) (ConditionalResult, error)
type RSSFetcher func(context.Context, Source, Validators) (ConditionalResult, error)
type Option func(*Service)

func WithConditionalFeeds(fetch ConditionalFetcher) Option {
	return func(s *Service) { s.conditional = fetch }
}
func WithRSSFeeds(fetch RSSFetcher) Option  { return func(s *Service) { s.rss = fetch } }
func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

type RefreshPolicy struct {
	Enabled         bool  `json:"enabled"`
	IntervalSeconds int64 `json:"intervalSeconds"`
}

// RetryDelay is supplied by the composition root, keeping HTTP adapter errors out of this module.
type RetryDelay interface{ RetryDelaySeconds() int64 }

type HTTPFailure interface{ HTTPStatusCode() int }

func (s *Service) ConfigureRefresh(ctx context.Context, id string, policy RefreshPolicy) (Source, error) {
	if !SupportsRefresh(id) || policy.IntervalSeconds < 900 || policy.IntervalSeconds > 604800 {
		return Source{}, ErrInvalid
	}
	release, err := s.acquire(id)
	if err != nil {
		return Source{}, err
	}
	defer release()
	record, err := s.repository.GetSubscriptionRefresh(ctx, id)
	if err != nil {
		return Source{}, err
	}
	// Saving the same policy must not postpone an already scheduled check.
	if record.State.Enabled != policy.Enabled || record.State.IntervalSeconds != policy.IntervalSeconds {
		previousNext := record.State.NextCheckAt
		record.State.Enabled = policy.Enabled
		record.State.IntervalSeconds = policy.IntervalSeconds
		record.State.NextCheckAt = 0
		if policy.Enabled {
			record.State.NextCheckAt = s.now().Unix() + policy.IntervalSeconds
			// Changing the interval must not shorten an outstanding failure backoff.
			if record.State.Failures > 0 {
				record.State.NextCheckAt = max(record.State.NextCheckAt, previousNext)
			}
		} else if record.State.Failures > 0 {
			// Disabled sources are never scheduled; retain the delay for re-enabling.
			record.State.NextCheckAt = previousNext
		}
	}
	detail, err := s.repository.CommitSubscriptionRefresh(ctx, id, nil, 0, record)
	return detail.Source, err
}
func backoff(record RefreshRecord) int64 {
	delay := int64(60) << min(max(record.State.Failures-1, 0), 10)
	return min(max(delay, record.State.IntervalSeconds), 604800)
}
func (s *Service) updateFeed(ctx context.Context, detail Detail) (Detail, error) {
	id := detail.Source.ID
	record, err := s.repository.GetSubscriptionRefresh(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	record.State.LastAttemptAt = s.now().Unix()
	if record.State.Enabled {
		record.State.NextCheckAt = record.State.LastAttemptAt + backoff(record)
	}
	if _, err = s.repository.CommitSubscriptionRefresh(ctx, id, nil, 0, record); err != nil {
		return Detail{}, err
	}
	validators := record.Validators
	if detail.Source.LastUpdatedAt == 0 {
		validators = Validators{}
	}
	var result ConditionalResult
	if strings.HasPrefix(id, "rss:feed:") && s.rss != nil {
		result, err = s.rss(ctx, detail.Source, validators)
	} else if s.conditional != nil {
		result, err = s.conditional(ctx, detail.Source.CreatorID, validators)
	} else {
		result.Snapshot, err = s.fetch(ctx, detail.Source.CreatorID)
	}
	if ctx.Err() != nil {
		return Detail{}, ctx.Err()
	}
	if err == nil && result.NotModified && detail.Source.LastUpdatedAt == 0 {
		err = ErrSnapshot
	}
	if err == nil && !result.NotModified {
		result.Snapshot, err = normalizeSnapshot(id, result.Snapshot)
	}
	if err != nil {
		record.State.Failures = min(record.State.Failures+1, 20)
		record.State.Message = "订阅源更新失败，保留上次内容；请检查地址与网络。"
		if strings.HasPrefix(id, "youtube:") {
			record.State.Message = "YouTube 频道更新失败，保留上次内容；请检查频道与网络。"
		}
		var timeout interface{ Timeout() bool }
		if errors.As(err, &timeout) && timeout.Timeout() {
			record.State.Message = "等待订阅源响应超时，已有内容保留；请检查源生成耗时与上游网络，稍后再更新。"
		}
		var httpFailure HTTPFailure
		if errors.As(err, &httpFailure) {
			switch status := httpFailure.HTTPStatusCode(); {
			case status == 401 || status == 403:
				record.State.Message = "订阅源拒绝访问，请检查访问密钥或鉴权配置。"
			case status == 404:
				record.State.Message = "订阅源返回 HTTP 404，请检查路由与 RSSHub 版本。"
			case status >= 500 && status <= 599:
				record.State.Message = "订阅源返回 HTTP " + strconv.Itoa(status) + "，请检查 RSSHub 或订阅服务日志；已有内容保留。"
			}
		}
		var platform *discovery.PlatformError
		if errors.As(err, &platform) && platform.Restricted {
			record.State.Message = "订阅源暂时限制请求，已延后检查。"
		}
		delay := backoff(record)
		var retry RetryDelay
		if errors.As(err, &retry) {
			delay = max(delay, min(max(retry.RetryDelaySeconds(), 0), 604800))
		}
		if record.State.Enabled {
			record.State.NextCheckAt = s.now().Unix() + delay
		}
		if _, commitErr := s.repository.CommitSubscriptionRefresh(ctx, id, nil, 0, record); commitErr != nil {
			return Detail{}, commitErr
		}
		return Detail{}, err
	}
	old := Snapshot{Items: detail.Items, Title: detail.Source.SourceTitle, Total: detail.Source.Total, HasMore: detail.Source.HasMore}
	unchanged := result.NotModified || detail.Source.LastUpdatedAt != 0 && reflect.DeepEqual(old, result.Snapshot)
	record.Validators = result.Validators
	record.State.LastCheckedAt = s.now().Unix()
	record.State.Failures = 0
	record.State.Message = ""
	record.State.NextCheckAt = 0
	if record.State.Enabled {
		record.State.NextCheckAt = record.State.LastCheckedAt + record.State.IntervalSeconds
	}
	var changed *Snapshot
	if !unchanged {
		changed = &result.Snapshot
		record.State.ContentRevision++
	}
	saved, err := s.repository.CommitSubscriptionRefresh(ctx, id, changed, s.now().Unix(), record)
	saved.NotModified = unchanged
	return saved, err
}

// RefreshDue refreshes every source that is due at the start of the tick. The
// worker count matches the service-wide update gate, so a slow source cannot
// block the rest of the due set while manual updates still share the same cap.
func (s *Service) RefreshDue(ctx context.Context, now time.Time) error {
	sources, err := s.List(ctx)
	if err != nil {
		return err
	}
	due := make([]string, 0, len(sources))
	for i := range sources {
		source := &sources[i]
		if !SupportsRefresh(source.ID) || !source.Refresh.Enabled || source.Active || source.Refresh.NextCheckAt > now.Unix() {
			continue
		}
		due = append(due, source.ID)
	}
	if len(due) == 0 {
		return nil
	}

	jobs := make(chan string)
	workerCount := min(len(due), 2)
	var workers sync.WaitGroup
	var failuresMu sync.Mutex
	var failures []error
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case id, ok := <-jobs:
					if !ok {
						return
					}
					if _, updateErr := s.update(ctx, id, &now); updateErr != nil {
						failuresMu.Lock()
						failures = append(failures, updateErr)
						failuresMu.Unlock()
					}
				}
			}
		}()
	}
dispatch:
	for _, id := range due {
		select {
		case jobs <- id:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	workers.Wait()
	return errors.Join(failures...)
}
func (s *Service) StartRefresh() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || s.closed {
		return
	}
	s.started = true
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
				_ = s.RefreshDue(s.ctx, s.now())
			}
		}
	}()
}
func (s *Service) Close() { s.mu.Lock(); s.closed = true; s.cancel(); s.mu.Unlock(); s.wg.Wait() }

// Both channel Atom and generic RSS sources support bounded conditional checks.
func SupportsRefresh(id string) bool {
	return strings.HasPrefix(id, "rss:feed:") || strings.HasPrefix(id, "youtube:channel:")
}
