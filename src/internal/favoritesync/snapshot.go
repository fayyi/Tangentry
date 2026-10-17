package favoritesync

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	"tangentry/internal/content"
	"tangentry/internal/discovery"
)

func (s *Service) run(ctx context.Context, previous Source) {
	source, err := s.scan(ctx, previous)
	if err != nil {
		previous.Active = false
		previous.Message = "同步未完成，上次成功内容已保留。"
		var platform *discovery.PlatformError
		if errors.As(err, &platform) {
			previous.Message = platform.Message
			if platform.Restricted && ctx.Err() == nil {
				previous.RestrictedUntil = s.restrictAccount()
				previous.Message = "B站暂时限制收藏请求，暂停同步 15 分钟。上次成功内容与待下载记录已保留。"
			}
		}
		if errors.Is(err, ErrIncomplete) {
			previous.Message = "收藏夹返回不完整，或超过 4000 条。请稍后重试或选择较小的收藏夹。"
		}
		if errors.Is(err, context.Canceled) || s.ctx.Err() != nil {
			previous.Message = "同步已取消，上次成功内容已保留。"
		}
		_ = s.repo.PutFavoriteSource(context.Background(), previous)
		return
	}
	source.Active = false
	source.RestrictedUntil = 0
	// A complete remote snapshot is persisted before admitting any download.
	if err := s.repo.PutFavoriteSource(ctx, source); err != nil {
		previous.Active = false
		previous.Message = "收藏缓存未能写入，未开始新的下载。"
		_ = s.repo.PutFavoriteSource(context.Background(), previous)
		return
	}
	if source.Policy.AutoDownload && ctx.Err() == nil {
		s.pump(ctx, &source)
	}
}

func (s *Service) scan(ctx context.Context, previous Source) (Source, error) {
	result := previous
	// Pruning a new snapshot must not alter the confirmed download records
	// restored when its database write fails.
	result.Transfers = maps.Clone(previous.Transfers)
	result.Items = []content.Item{}
	result.Excluded = 0
	seen := make(map[string]bool)
	entries := 0
	var expected Folder
	for page := 1; page <= MaxItems/20+1; page++ {
		if page > 1 {
			timer := time.NewTimer(s.pageDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return Source{}, ctx.Err()
			case <-timer.C:
			}
		}
		remote, err := s.remote.Page(ctx, previous.FolderID, page)
		if err != nil {
			return Source{}, err
		}
		if !validFolder(remote.Folder) || remote.Folder.ID != previous.FolderID || remote.Entries < 0 || remote.Entries > 20 || remote.Excluded < 0 || remote.Excluded > remote.Entries || len(remote.Items) != remote.Entries-remote.Excluded {
			return Source{}, ErrIncomplete
		}
		if page == 1 {
			expected = remote.Folder
		} else if remote.Folder.Count != expected.Count {
			return Source{}, ErrIncomplete
		}
		if remote.HasMore && remote.Entries == 0 {
			return Source{}, ErrIncomplete
		}
		added := 0
		for _, item := range remote.Items {
			platform, bvid, canonical, err := discovery.VideoURL(item.URL)
			if err != nil || platform != "bilibili" || item.ID != "bilibili:"+bvid || item.PlatformID != bvid || item.Kind != "video" || content.Validate(item) != nil {
				return Source{}, ErrIncomplete
			}
			if !seen[item.ID] {
				item.URL = canonical
				seen[item.ID] = true
				result.Items = append(result.Items, item)
				added++
			}
		}
		if added != len(remote.Items) || (remote.HasMore && remote.Excluded == 0 && added == 0) {
			return Source{}, ErrIncomplete
		}
		entries += remote.Entries
		result.Excluded += remote.Excluded
		if entries > MaxItems || entries > expected.Count {
			return Source{}, ErrIncomplete
		}
		if !remote.HasMore {
			if entries != expected.Count {
				return Source{}, ErrIncomplete
			}
			return completeSnapshot(previous, result, expected, seen), nil
		}
	}
	return Source{}, ErrIncomplete
}

func completeSnapshot(previous, result Source, expected Folder, seen map[string]bool) Source {
	result.Title = expected.Title
	result.LastSuccessAt = time.Now().Unix()
	result.Message = fmt.Sprintf("已同步 %d 个视频", len(result.Items))
	if result.Excluded > 0 {
		result.Message += fmt.Sprintf("，跳过 %d 条失效或其他类型内容", result.Excluded)
	}
	old := make(map[string]bool)
	for _, item := range previous.Items {
		old[item.ID] = true
	}
	pending := make(map[string]bool)
	eligible := make(map[string]bool)
	filtered := 0
	for _, item := range result.Items {
		eligible[item.ID] = previous.Policy.matchesDownload(item)
		if !eligible[item.ID] {
			filtered++
		}
	}
	if filtered > 0 {
		result.Message += fmt.Sprintf("；%d 个视频未匹配自动下载规则", filtered)
	}
	result.Pending = []string{}
	for _, id := range previous.Pending {
		if eligible[id] && !pending[id] {
			pending[id] = true
			result.Pending = append(result.Pending, id)
		}
	}
	if previous.Policy.AutoDownload {
		for _, item := range result.Items {
			if eligible[item.ID] && !old[item.ID] && !pending[item.ID] {
				pending[item.ID] = true
				result.Pending = append(result.Pending, item.ID)
			}
		}
	}
	if result.Transfers == nil {
		result.Transfers = make(map[string]Transfer)
	}
	for id := range result.Transfers {
		if !seen[id] {
			delete(result.Transfers, id)
		}
	}
	return result
}
