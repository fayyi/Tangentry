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

const DefaultInterval = int64(7200)

// RefreshStatus is deliberately small and stable: it is safe to persist and
// lets clients distinguish an empty feed from a source that needs attention.
type RefreshStatus string

const (
	StatusHealthy      RefreshStatus = "healthy"
	StatusNeverChecked RefreshStatus = "never_checked"
	StatusStale        RefreshStatus = "stale"
	StatusAuthRequired RefreshStatus = "auth_required"
	StatusRateLimited  RefreshStatus = "rate_limited"
	StatusNotFound     RefreshStatus = "not_found"
	StatusUpstreamErr  RefreshStatus = "upstream_error"
	StatusPaused       RefreshStatus = "paused"
)

const staleAfter = int64(30 * 24 * 60 * 60)

// Public state contains no feed validators or network credentials.
type RefreshState struct {
	Enabled         bool          `json:"enabled"`
	IntervalSeconds int64         `json:"intervalSeconds"`
	LastAttemptAt   int64         `json:"lastAttemptAt"`
	LastFailureAt   int64         `json:"lastFailureAt"`
	LastCheckedAt   int64         `json:"lastCheckedAt"`
	NextCheckAt     int64         `json:"nextCheckAt"`
	Failures        int           `json:"failures"`
	Message         string        `json:"message,omitempty"`
	Status          RefreshStatus `json:"status"`
	SuggestedAction string        `json:"suggestedAction,omitempty"`
	ContentRevision int64         `json:"contentRevision"`
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
	return RefreshRecord{State: RefreshState{IntervalSeconds: DefaultInterval, Status: StatusNeverChecked}}
}

// DefaultRefreshFor applies the product default to every saved source. The
// scheduler uses conditional validators where a source kind supports them and
// falls back to its regular snapshot fetcher otherwise.
func DefaultRefreshFor(id string) RefreshRecord {
	record := DefaultRefresh()
	record.State.Enabled = validID(id)
	if !record.State.Enabled {
		record.State.Status = StatusPaused
	}
	return record
}

// NormalizeRefreshState repairs records written before health statuses were
// introduced and keeps disabled sources explicitly paused.
func NormalizeRefreshState(state *RefreshState) {
	if state.IntervalSeconds == 0 {
		state.IntervalSeconds = DefaultInterval
	}
	if !state.Enabled {
		state.Status = StatusPaused
		state.SuggestedAction = "手动更新来源，或开启自动刷新。"
		return
	}
	if state.Status == "" {
		switch {
		case state.Failures > 0:
			state.Status = StatusUpstreamErr
		case state.LastCheckedAt == 0:
			state.Status = StatusNeverChecked
		default:
			state.Status = StatusHealthy
		}
	}
	if state.SuggestedAction == "" {
		state.SuggestedAction = suggestedAction(state.Status)
	}
}

func suggestedAction(status RefreshStatus) string {
	switch status {
	case StatusHealthy:
		return "无需处理，按计划检查。"
	case StatusNeverChecked:
		return "立即更新一次，确认来源可用。"
	case StatusStale:
		return "检查来源是否仍在更新，必要时打开来源页面。"
	case StatusAuthRequired:
		return "去设置重新验证平台登录信息。"
	case StatusRateLimited:
		return "等待下一次检查，避免连续手动重试。"
	case StatusNotFound:
		return "检查订阅地址或 RSSHub 路由。"
	case StatusPaused:
		return "手动更新来源，或开启自动刷新。"
	default:
		return "检查来源地址与网络后重试。"
	}
}

// StatusFor returns the user-facing health category for a persisted state.
// now is injected by callers/tests so stale classification is deterministic.
func StatusFor(state RefreshState, lastUpdatedAt int64, now int64) RefreshStatus {
	NormalizeRefreshState(&state)
	if !state.Enabled {
		return StatusPaused
	}
	if state.Failures > 0 && state.Status != StatusHealthy && state.Status != StatusStale && state.Status != StatusNeverChecked {
		return state.Status
	}
	if state.LastCheckedAt == 0 {
		return StatusNeverChecked
	}
	if lastUpdatedAt > 0 && now > lastUpdatedAt && now-lastUpdatedAt >= staleAfter {
		return StatusStale
	}
	return StatusHealthy
}

func ApplyHealth(source *Source, now int64) {
	if source == nil {
		return
	}
	NormalizeRefreshState(&source.Refresh)
	source.Refresh.Status = StatusFor(source.Refresh, source.LastUpdatedAt, now)
	source.Refresh.SuggestedAction = suggestedAction(source.Refresh.Status)
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
	NormalizeRefreshState(&record.State)
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
	if !record.State.Enabled {
		record.State.Status = StatusPaused
		record.State.SuggestedAction = suggestedAction(StatusPaused)
	} else if record.State.Status == StatusPaused {
		record.State.Status = StatusNeverChecked
		record.State.SuggestedAction = suggestedAction(StatusNeverChecked)
	}
	detail, err := s.repository.CommitSubscriptionRefresh(ctx, id, nil, 0, record)
	ApplyHealth(&detail.Source, s.now().Unix())
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
	NormalizeRefreshState(&record.State)
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
	} else if strings.HasPrefix(id, "rss:feed:") && s.conditional != nil {
		// Keep the lightweight conditional fetch hook usable for tests and
		// compositions that do not provide the identity-aware RSS adapter.
		result, err = s.conditional(ctx, detail.Source.CreatorID, validators)
	} else if strings.HasPrefix(id, "youtube:channel:") && s.conditional != nil {
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
		record.State.LastFailureAt = s.now().Unix()
		record.State.Status = StatusUpstreamErr
		record.State.SuggestedAction = suggestedAction(StatusUpstreamErr)
		record.State.Message = "订阅源更新失败，保留上次内容；请检查地址与网络。"
		if strings.HasPrefix(id, "youtube:") {
			record.State.Message = "YouTube 频道更新失败，保留上次内容；请检查频道与网络。"
		}
		var timeout interface{ Timeout() bool }
		if errors.As(err, &timeout) && timeout.Timeout() {
			record.State.Status = StatusUpstreamErr
			record.State.Message = "等待订阅源响应超时，已有内容保留；请检查源生成耗时与上游网络，稍后再更新。"
		}
		var httpFailure HTTPFailure
		if errors.As(err, &httpFailure) {
			switch status := httpFailure.HTTPStatusCode(); {
			case status == 401 || status == 403:
				record.State.Status = StatusAuthRequired
				record.State.SuggestedAction = suggestedAction(StatusAuthRequired)
				record.State.Message = "订阅源拒绝访问，请检查访问密钥或鉴权配置。"
			case status == 404:
				record.State.Status = StatusNotFound
				record.State.SuggestedAction = suggestedAction(StatusNotFound)
				record.State.Message = "订阅源返回 HTTP 404，请检查路由与 RSSHub 版本。"
			case status == 429:
				record.State.Status = StatusRateLimited
				record.State.SuggestedAction = suggestedAction(StatusRateLimited)
				record.State.Message = "订阅源请求过于频繁，已延后检查；已有内容保留。"
			case status >= 500 && status <= 599:
				record.State.Status = StatusUpstreamErr
				record.State.SuggestedAction = suggestedAction(StatusUpstreamErr)
				record.State.Message = "订阅源返回 HTTP " + strconv.Itoa(status) + "，请检查 RSSHub 或订阅服务日志；已有内容保留。"
			}
		}
		var platform *discovery.PlatformError
		if errors.As(err, &platform) && platform.Restricted {
			record.State.Status = StatusRateLimited
			record.State.SuggestedAction = suggestedAction(StatusRateLimited)
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
	record.State.LastFailureAt = 0
	record.State.Failures = 0
	record.State.Message = ""
	record.State.Status = StatusHealthy
	record.State.SuggestedAction = suggestedAction(StatusHealthy)
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

// Every saved source can be scheduled. RSS and YouTube additionally support
// conditional validators; other source kinds use their regular fetcher.
func SupportsRefresh(id string) bool {
	return strings.HasPrefix(id, "rss:feed:") || strings.HasPrefix(id, "youtube:channel:") || strings.HasPrefix(id, "bilibili:creator:")
}
