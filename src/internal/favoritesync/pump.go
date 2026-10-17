package favoritesync

import (
	"context"
	"errors"
	"fmt"

	"tangentry/internal/content"
	"tangentry/internal/downloads"
)

// Retry failed/interrupted sync downloads at most three times. A user cancellation
// stays cancelled. Existing active or completed jobs are shared across folders.
func (s *Service) pump(ctx context.Context, source *Source) {
	jobs, err := s.downloader.List(ctx)
	if err != nil {
		source.Message = "收藏已同步，下载队列暂时无法读取。"
		_ = s.repo.PutFavoriteSource(context.Background(), *source)
		return
	}
	byID, byURL, latestByURL := indexJobs(jobs)
	current := make(map[string]content.Item)
	pending := make(map[string]bool)
	for _, item := range source.Items {
		current[item.ID] = item
	}
	for _, id := range source.Pending {
		pending[id] = true
	}
	// Retry in snapshot order, not map order, so a bounded run is predictable.
	for _, item := range source.Items {
		id := item.ID
		transfer, tracked := source.Transfers[id]
		if !tracked {
			continue
		}
		active, shared := byURL[item.URL]
		shared = shared && (active.State == "queued" || active.State == "running")
		job, known := byID[transfer.JobID]
		retry := known && (job.State == "failed" || job.State == "interrupted" || job.State == "completed") && transfer.Attempts < 3
		if (shared || retry) && !pending[id] {
			if item, present := current[id]; present && source.Policy.matchesDownload(item) {
				source.Pending = append(source.Pending, id)
				pending[id] = true
			}
		}
	}

	remaining := make([]string, 0)
	admitted := 0
	queue := append([]string(nil), source.Pending...)
	for index, id := range queue {
		if ctx.Err() != nil {
			remaining = append(remaining, queue[index:]...)
			break
		}
		item, exists := current[id]
		if !exists || !source.Policy.matchesDownload(item) {
			continue
		}
		if job, exists := byURL[item.URL]; exists && (job.State == "queued" || job.State == "running") {
			source.Transfers[id] = Transfer{JobID: job.ID, Attempts: source.Transfers[id].Attempts}
			continue
		}
		if transfer, ok := source.Transfers[id]; ok {
			if job, exists := byID[transfer.JobID]; transfer.Attempts >= 3 || (exists && job.State == "cancelled") {
				continue
			}
		}
		if job, exists := latestByURL[item.URL]; exists && job.State == "cancelled" {
			source.Transfers[id] = Transfer{JobID: job.ID, Attempts: source.Transfers[id].Attempts}
			continue
		}
		if _, err := s.downloader.CompletedPath(ctx, item.URL); err == nil {
			if job, exists := byURL[item.URL]; exists && job.State == "completed" {
				source.Transfers[id] = Transfer{JobID: job.ID, Attempts: source.Transfers[id].Attempts}
			}
			continue
		} else if !errors.Is(err, downloads.ErrNotFound) {
			remaining = append(remaining, queue[index:]...)
			source.Message = "收藏已同步，无法确认本机文件，下载暂缓。"
			break
		}
		if admitted >= source.Policy.MaxPerRun {
			remaining = append(remaining, id)
			continue
		}
		job, err := s.downloader.Start(ctx, downloads.Request{URL: item.URL, Title: item.Title, MaxHeight: source.Policy.MaxHeight})
		if err != nil {
			remaining = append(remaining, queue[index:]...)
			source.Message = "收藏已同步，下载暂缓，请检查媒体工具或下载队列。"
			break
		}
		admitted++
		source.Transfers[id] = Transfer{JobID: job.ID, Attempts: source.Transfers[id].Attempts + 1}
		byURL[item.URL] = job
		// Persist each admission: a crash after Start is recovered by URL deduplication.
		source.Pending = append(remaining, queue[index+1:]...)
		if err := s.repo.PutFavoriteSource(ctx, *source); err != nil {
			return
		}
		// The iteration range has its own slice header; source.Pending is only a checkpoint.
	}
	source.Pending = remaining
	if admitted > 0 {
		source.Message += fmt.Sprintf("；%d 个视频已加入下载队列", admitted)
	}
	_ = s.repo.PutFavoriteSource(context.Background(), *source)
}

func indexJobs(jobs []downloads.Job) (byID, byURL, latestByURL map[string]downloads.Job) {
	byID = make(map[string]downloads.Job)
	byURL = make(map[string]downloads.Job)
	latestByURL = make(map[string]downloads.Job)
	for _, job := range jobs {
		byID[job.ID] = job
		latest, exists := latestByURL[job.URL]
		if !exists || job.CreatedAt > latest.CreatedAt || job.CreatedAt == latest.CreatedAt && (job.UpdatedAt > latest.UpdatedAt || job.UpdatedAt == latest.UpdatedAt && job.State == "cancelled") {
			latestByURL[job.URL] = job
		}
		old, exists := byURL[job.URL]
		if !exists || jobPriority(job.State) > jobPriority(old.State) || (jobPriority(job.State) == jobPriority(old.State) && job.CreatedAt > old.CreatedAt) {
			byURL[job.URL] = job
		}
	}
	return byID, byURL, latestByURL
}

func jobPriority(state string) int {
	switch state {
	case "queued", "running":
		return 3
	case "completed":
		return 2
	default:
		return 1
	}
}
