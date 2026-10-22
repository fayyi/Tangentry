package favoritesync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"tangentry/internal/content"
	"tangentry/internal/downloads"
)

// Retry failed/interrupted sync downloads at most three times. A user cancellation
// stays cancelled. Folder jobs keep independent destinations; an active manual
// job can still be adopted to avoid downloading the same media concurrently.
func (s *Service) pump(ctx context.Context, source *Source) {
	*source = NormalizeSource(*source)
	jobs, err := s.downloader.List(ctx)
	if err != nil {
		source.Message = "收藏已同步，下载队列暂时无法读取。"
		_ = s.repo.PutFavoriteSource(context.Background(), *source)
		return
	}
	byID, byKey, latestByKey, activeManual := indexJobs(jobs)
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
		directory := sourceDirectory(source)
		job, known := byID[transfer.JobID]
		if known && sameDownload(job, item.URL, source.Policy.MaxHeight, source.Policy.Download) && job.Subdirectory != "" {
			directory = job.Subdirectory
		}
		key := jobKey(item.URL, source.Policy.MaxHeight, source.Policy.Download, directory)
		active, shared := byKey[key]
		shared = shared && (active.State == "queued" || active.State == "running")
		if !shared {
			active, shared = activeManual[manualKey(item.URL, source.Policy.MaxHeight, source.Policy.Download)]
		}
		transferKey := transfer.Key
		if transferKey == "" && known {
			transferKey = jobKey(job.URL, job.MaxHeight, job.Options, job.Subdirectory)
		}
		legacyMatch := known && transfer.Key == "" && job.Subdirectory == "" && sameDownload(job, item.URL, source.Policy.MaxHeight, source.Policy.Download)
		retry := known && (transferKey == key || legacyMatch) && (job.State == "failed" || job.State == "interrupted" || job.State == "completed") && transfer.Attempts < 3
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
		directory := sourceDirectory(source)
		transfer := source.Transfers[id]
		if previous, exists := byID[transfer.JobID]; exists && sameDownload(previous, item.URL, source.Policy.MaxHeight, source.Policy.Download) && previous.Subdirectory != "" {
			directory = previous.Subdirectory
		}
		key := jobKey(item.URL, source.Policy.MaxHeight, source.Policy.Download, directory)
		job, exists := byKey[key]
		if exists && (job.State == "queued" || job.State == "running") {
			source.Transfers[id] = Transfer{JobID: job.ID, Attempts: source.Transfers[id].Attempts, Key: key}
			continue
		}
		if job, exists := activeManual[manualKey(item.URL, source.Policy.MaxHeight, source.Policy.Download)]; exists {
			source.Transfers[id] = Transfer{JobID: job.ID, Attempts: source.Transfers[id].Attempts, Key: key}
			continue
		}
		if transfer, ok := source.Transfers[id]; ok {
			job, exists := byID[transfer.JobID]
			transferKey := transfer.Key
			if transferKey == "" && exists {
				transferKey = jobKey(job.URL, job.MaxHeight, job.Options, job.Subdirectory)
			}
			legacyMatch := exists && transfer.Key == "" && job.Subdirectory == "" && sameDownload(job, item.URL, source.Policy.MaxHeight, source.Policy.Download)
			if (transferKey == key || legacyMatch) && (transfer.Attempts >= 3 || (exists && job.State == "cancelled")) {
				continue
			}
		}
		job, exists = latestByKey[key]
		if exists && job.State == "cancelled" {
			source.Transfers[id] = Transfer{JobID: job.ID, Attempts: source.Transfers[id].Attempts}
			continue
		}
		if _, err := completedPath(ctx, s.downloader, item.URL, source.Policy.MaxHeight, source.Policy.Download, directory); err == nil {
			if job, exists := byKey[key]; exists && job.State == "completed" {
				source.Transfers[id] = Transfer{JobID: job.ID, Attempts: source.Transfers[id].Attempts, Key: key}
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
		outputDirectory := videoDirectoryName(item)
		if transfer, tracked := source.Transfers[id]; tracked {
			if previous, exists := byID[transfer.JobID]; exists && previous.Subdirectory == directory && sameDownload(previous, item.URL, source.Policy.MaxHeight, source.Policy.Download) && previous.OutputDirectory != "" {
				outputDirectory = previous.OutputDirectory
			}
		}
		job, err := s.downloader.Start(ctx, downloads.Request{URL: item.URL, Title: item.Title, MaxHeight: source.Policy.MaxHeight, Options: source.Policy.Download, Subdirectory: directory, OutputDirectory: outputDirectory})
		if err != nil {
			remaining = append(remaining, queue[index:]...)
			source.Message = "收藏已同步，下载暂缓，请检查媒体工具或下载队列。"
			break
		}
		admitted++
		source.Transfers[id] = Transfer{JobID: job.ID, Attempts: source.Transfers[id].Attempts + 1, Key: key}
		byKey[key] = job
		latestByKey[key] = job
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

func indexJobs(jobs []downloads.Job) (byID, byKey, latestByKey, activeManual map[string]downloads.Job) {
	byID = make(map[string]downloads.Job)
	byKey = make(map[string]downloads.Job)
	latestByKey = make(map[string]downloads.Job)
	activeManual = make(map[string]downloads.Job)
	for _, job := range jobs {
		byID[job.ID] = job
		key := jobKey(job.URL, job.MaxHeight, job.Options, job.Subdirectory)
		latest, exists := latestByKey[key]
		if !exists || job.CreatedAt > latest.CreatedAt || job.CreatedAt == latest.CreatedAt && (job.UpdatedAt > latest.UpdatedAt || job.UpdatedAt == latest.UpdatedAt && job.State == "cancelled") {
			latestByKey[key] = job
		}
		old, exists := byKey[key]
		if !exists || jobPriority(job.State) > jobPriority(old.State) || (jobPriority(job.State) == jobPriority(old.State) && job.CreatedAt > old.CreatedAt) {
			byKey[key] = job
		}
		if job.Subdirectory == "" && (job.State == "queued" || job.State == "running") {
			manual := manualKey(job.URL, job.MaxHeight, job.Options)
			oldManual, exists := activeManual[manual]
			if !exists || job.CreatedAt > oldManual.CreatedAt {
				activeManual[manual] = job
			}
		}
	}
	return byID, byKey, latestByKey, activeManual
}

type completedPathFor interface {
	CompletedPathFor(context.Context, string, int, downloads.Options, string) (string, error)
}

func completedPath(ctx context.Context, downloader Downloader, rawURL string, height int, options downloads.Options, subdirectory string) (string, error) {
	if finder, ok := downloader.(completedPathFor); ok {
		return finder.CompletedPathFor(ctx, rawURL, height, options, subdirectory)
	}
	return downloader.CompletedPath(ctx, rawURL)
}

func jobKey(rawURL string, height int, options downloads.Options, subdirectory string) string {
	_ = options.Normalize()
	encoded, _ := json.Marshal(options)
	return fmt.Sprintf("%s\n%d\n%s\n%s", rawURL, height, encoded, subdirectory)
}

func sameDownload(job downloads.Job, rawURL string, height int, options downloads.Options) bool {
	return job.URL == rawURL && job.MaxHeight == height && downloads.SameOptions(job.Options, options)
}

func manualKey(rawURL string, height int, options downloads.Options) string {
	return jobKey(rawURL, height, options, "")
}

func sourceDirectory(source *Source) string {
	if source.Policy.DirectoryName == "" {
		source.Policy.DirectoryName = folderDirectoryName(source.Title, source.FolderID)
	}
	return "B站收藏夹/" + source.Policy.DirectoryName
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
