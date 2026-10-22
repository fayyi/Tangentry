package favoritesync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"tangentry/internal/downloads"
)

type memoryRepo struct {
	mu              sync.Mutex
	records         map[string]Source
	failSnapshot    bool
	restrictedUntil int64
	failRestriction bool
}

func (r *memoryRepo) FavoriteRestriction(ctx context.Context) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.restrictedUntil, ctx.Err()
}
func (r *memoryRepo) ExtendFavoriteRestriction(ctx context.Context, until int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failRestriction {
		return errors.New("restriction fixture write failure")
	}
	r.restrictedUntil = max(r.restrictedUntil, until)
	return ctx.Err()
}

func (r *memoryRepo) GetFavoriteSource(ctx context.Context, id string) (Source, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.records[id]
	if !ok {
		return Source{}, ErrNotFound
	}
	var copy Source
	data, _ := json.Marshal(s)
	json.Unmarshal(data, &copy)
	return copy, ctx.Err()
}
func (r *memoryRepo) ListFavoriteSources(ctx context.Context) ([]Source, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := []Source{}
	for _, record := range r.records {
		var copy Source
		data, _ := json.Marshal(record)
		json.Unmarshal(data, &copy)
		s = append(s, copy)
	}
	sort.Slice(s, func(i, j int) bool { return s[i].FolderID < s[j].FolderID })
	return s, ctx.Err()
}
func (r *memoryRepo) PutFavoriteSource(ctx context.Context, s Source) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if r.failSnapshot && strings.HasPrefix(s.Message, "已同步") {
		return errors.New("explicit fixture storage failure")
	}
	var copy Source
	data, _ := json.Marshal(s)
	json.Unmarshal(data, &copy)
	r.records[s.FolderID] = copy
	return nil
}
func (r *memoryRepo) DeleteFavoriteSource(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.records[id]; !exists {
		return ErrNotFound
	}
	delete(r.records, id)
	return ctx.Err()
}

type fixtureRemote struct {
	mu       sync.Mutex
	pages    []Page
	calls    int
	failPage int
	failure  error
	gate     chan struct{}
}

type countedFolders struct {
	Remote
	calls   int
	failure error
}

func (r *countedFolders) Folders(ctx context.Context) ([]Folder, error) {
	r.calls++
	if r.failure != nil {
		return nil, r.failure
	}
	return []Folder{{ID: "12345", Title: "Fixture", Count: 0}}, ctx.Err()
}

func TestRestrictedFolderResponseStartsPersistentCoolingOnlyForPlatformRestriction(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		failure    error
		cancel     bool
		writeFails bool
		limited    bool
	}{
		{"restricted", &discovery.PlatformError{Restricted: true, Message: "fixture restriction"}, false, false, true},
		{"failed persistence", &discovery.PlatformError{Restricted: true}, false, true, true},
		{"login", &discovery.PlatformError{Message: "fixture login"}, false, false, false},
		{"network", errors.New("fixture network"), false, false, false},
		{"cancelled", &discovery.PlatformError{Restricted: true}, true, false, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			repo := &memoryRepo{records: map[string]Source{}, failRestriction: scenario.writeFails}
			remote := &countedFolders{failure: scenario.failure}
			service := New(repo, remote, nil)
			ctx, cancel := context.WithCancel(context.Background())
			if scenario.cancel {
				cancel()
			}
			_, _ = service.Folders(ctx)
			cancel()
			before := remote.calls
			_, _ = service.Folders(context.Background())
			if scenario.limited && remote.calls != before || !scenario.limited && remote.calls != before+1 {
				t.Fatal("wrong cooling behavior", remote.calls, before)
			}
			service.Close()
			if scenario.limited && !scenario.writeFails {
				reopened := New(repo, remote, nil)
				defer reopened.Close()
				if _, err := reopened.Folders(context.Background()); err == nil || remote.calls != before {
					t.Fatal("restart bypassed cooling", err, remote.calls)
				}
			}
		})
	}
}

func TestFolderListingHonorsAccountRestrictionWithoutRemoteRequests(t *testing.T) {
	repo := &memoryRepo{records: map[string]Source{}, restrictedUntil: time.Now().Add(time.Hour).Unix()}
	remote := &countedFolders{}
	service := New(repo, remote, nil)
	defer service.Close()
	for i := 0; i < 2; i++ {
		_, err := service.Folders(context.Background())
		var platform *discovery.PlatformError
		if !errors.As(err, &platform) || !platform.Restricted || remote.calls != 0 {
			t.Fatal("restricted folder listing contacted platform", err, remote.calls)
		}
	}
	repo.mu.Lock()
	repo.restrictedUntil = 0
	repo.mu.Unlock()
	service.mu.Lock()
	service.restrictedUntil = time.Now().Add(time.Hour).Unix()
	service.mu.Unlock()
	if _, err := service.Folders(context.Background()); err == nil || remote.calls != 0 {
		t.Fatal("in-memory restriction bypassed", err, remote.calls)
	}
	service.mu.Lock()
	service.restrictedUntil = time.Now().Add(-time.Minute).Unix()
	service.mu.Unlock()
	folders, err := service.Folders(context.Background())
	if err != nil || len(folders) != 1 || remote.calls != 1 {
		t.Fatal("expired restriction did not permit explicit listing", folders, err, remote.calls)
	}
}

func (r *fixtureRemote) Folders(ctx context.Context) ([]Folder, error) {
	return []Folder{{ID: "12345", Title: "明确测试收藏夹", Count: 6}}, ctx.Err()
}
func (r *fixtureRemote) Page(ctx context.Context, id string, page int) (Page, error) {
	r.mu.Lock()
	r.calls++
	fail := r.failPage == page
	gate := r.gate
	failure := r.failure
	result := r.pages[page-1]
	r.mu.Unlock()
	if gate != nil {
		select {
		case <-ctx.Done():
			return Page{}, ctx.Err()
		case <-gate:
		}
	}
	if fail {
		if failure != nil {
			return Page{}, failure
		}
		return Page{}, errors.New("upstream fixture failure includes private URL")
	}
	result.Folder.ID = id
	return result, ctx.Err()
}

func TestRestrictionPausesAllFoldersAndSurvivesServiceRestart(t *testing.T) {
	s, repo, remote, downloads := newFixture(t)
	ctx := context.Background()
	p := DefaultPolicy()
	p.Enabled, p.AutoDownload = true, true
	for _, id := range []string{"12345", "23456"} {
		if _, err := s.Create(ctx, Input{Query: id, Policy: p}); err != nil {
			t.Fatal(err)
		}
	}
	before := syncOnce(t, s, "12345")
	remote.mu.Lock()
	remote.failPage = 1
	remote.failure = &discovery.PlatformError{Restricted: true, Message: "restricted fixture"}
	remote.mu.Unlock()
	limited := syncOnce(t, s, "12345")
	if limited.RestrictedUntil < time.Now().Add(14*time.Minute).Unix() || limited.LastSuccessAt != before.LastSuccessAt || len(limited.Pending) != len(before.Pending) || len(limited.Items) != len(before.Items) || downloads.started != 3 {
		t.Fatal("restriction changed confirmed content or downloads", limited)
	}
	remote.mu.Lock()
	calls := remote.calls
	remote.mu.Unlock()
	s.tick(time.Now())
	if _, err := s.Start(ctx, "23456"); err == nil {
		t.Fatal("another folder bypassed account cooldown")
	}
	s.Close()
	restarted := New(repo, remote, downloads)
	t.Cleanup(restarted.Close)
	if _, err := restarted.Start(ctx, "12345"); err == nil {
		t.Fatal("restart bypassed persisted cooldown")
	}
	remote.mu.Lock()
	if remote.calls != calls {
		t.Fatal("cooldown performed remote calls", remote.calls, calls)
	}
	remote.failPage = 0
	remote.mu.Unlock()
	limited.RestrictedUntil = time.Now().Add(-time.Second).Unix()
	repo.mu.Lock()
	repo.restrictedUntil = limited.RestrictedUntil
	repo.mu.Unlock()
	if err := repo.PutFavoriteSource(ctx, limited); err != nil {
		t.Fatal(err)
	}
	result := syncOnce(t, restarted, "12345")
	if result.RestrictedUntil != 0 || downloads.started != 6 {
		t.Fatal("expired restriction did not resume pending work", result, downloads.started)
	}
}

func TestRemovingRestrictedFolderCannotResumeOtherFolders(t *testing.T) {
	for _, writeFails := range []bool{false, true} {
		t.Run(fmt.Sprint(writeFails), func(t *testing.T) {
			s, repo, remote, _ := newFixture(t)
			ctx := context.Background()
			s.Create(ctx, Input{Query: "12345"})
			s.Create(ctx, Input{Query: "23456"})
			repo.failRestriction = writeFails
			remote.failPage = 1
			remote.failure = &discovery.PlatformError{Restricted: true, Message: "restriction fixture"}
			syncOnce(t, s, "12345")
			if err := s.Remove(ctx, "12345"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Start(ctx, "23456"); err == nil {
				t.Fatal("source removal bypassed shared restriction")
			}
			remote.mu.Lock()
			defer remote.mu.Unlock()
			if remote.calls != 1 {
				t.Fatal("cooldown allowed another platform request", remote.calls)
			}
		})
	}
}

type fixtureDownloads struct {
	mu        sync.Mutex
	jobs      []downloads.Job
	started   int
	error     error
	fileError error
	filePath  string
}

func (d *fixtureDownloads) Start(ctx context.Context, r downloads.Request) (downloads.Job, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.error != nil {
		return downloads.Job{}, d.error
	}
	d.started++
	j := downloads.Job{Options: r.Options, ID: fmt.Sprint(d.started), URL: r.URL, Title: r.Title, Subdirectory: r.Subdirectory, OutputDirectory: r.OutputDirectory, State: "queued", CreatedAt: int64(d.started), MaxHeight: r.MaxHeight}
	d.jobs = append(d.jobs, j)
	return j, ctx.Err()
}
func (d *fixtureDownloads) List(ctx context.Context) ([]downloads.Job, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]downloads.Job(nil), d.jobs...), ctx.Err()
}
func (d *fixtureDownloads) CompletedPath(ctx context.Context, url string) (string, error) {
	if d.fileError != nil {
		return "", d.fileError
	}
	if d.filePath != "" {
		return d.filePath, nil
	}
	return "", downloads.ErrNotFound
}

func (d *fixtureDownloads) CompletedPathFor(ctx context.Context, rawURL string, height int, options downloads.Options, directory string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, job := range d.jobs {
		if job.URL != rawURL || job.State != "completed" || job.MaxHeight != height || !downloads.SameOptions(job.Options, options) || job.Subdirectory != directory {
			continue
		}
		if d.fileError != nil {
			return "", d.fileError
		}
		if d.filePath != "" {
			return d.filePath, nil
		}
	}
	return "", downloads.ErrNotFound
}

func TestCompletedRecordsRequireExistingFilesBeforeSkippingAdmission(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		path    string
		err     error
		want    int
		pending int
	}{
		{"missing", "", nil, 1, 0},
		{"present", "/fixture/existing.mp4", nil, 0, 0},
		{"unreadable", "", errors.New("fixture file read failure"), 0, 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			s, repo, remote, d := newFixture(t)
			item := video(0)
			remote.pages[0].Items = []content.Item{item}
			remote.pages[0].Entries = 1
			remote.pages[0].Folder.Count = 1
			p := DefaultPolicy()
			d.jobs = []downloads.Job{{Options: p.Download, ID: "old-completed", URL: item.URL, Subdirectory: "B站收藏夹/" + folderDirectoryName("测试收藏夹", "12345"), MaxHeight: p.MaxHeight, State: "completed"}}
			d.filePath, d.fileError = scenario.path, scenario.err
			p.AutoDownload = true
			if _, err := s.Create(context.Background(), Input{Query: "12345", Policy: p}); err != nil {
				t.Fatal(err)
			}
			source := syncOnce(t, s, "12345")
			if d.started != scenario.want || len(source.Pending) != scenario.pending {
				t.Fatalf("started=%d pending=%d", d.started, len(source.Pending))
			}
			// A later scan must also verify files for a previously tracked transfer.
			d.jobs = []downloads.Job{{Options: p.Download, ID: "old-completed", URL: item.URL, Subdirectory: "B站收藏夹/" + folderDirectoryName("测试收藏夹", "12345"), MaxHeight: p.MaxHeight, State: "completed"}}
			d.started = 0
			source.Pending = nil
			source.Transfers = map[string]Transfer{item.ID: {JobID: "old-completed", Attempts: 1}}
			if err := repo.PutFavoriteSource(context.Background(), source); err != nil {
				t.Fatal(err)
			}
			source = syncOnce(t, s, "12345")
			if d.started != scenario.want || len(source.Pending) != scenario.pending {
				t.Fatalf("tracked: started=%d pending=%d", d.started, len(source.Pending))
			}
		})
	}
}

func TestFavoriteDownloadsUseReadableIsolatedHierarchyAndSafeNames(t *testing.T) {
	s, _, _, d := newFixture(t)
	ctx := context.Background()
	policy := DefaultPolicy()
	policy.AutoDownload = true
	policy.DirectoryName = "自定义 收藏"
	policy.MaxHeight = 720
	policy.Download = downloads.Options{Container: "mkv", SubtitleLanguages: []string{"zh-Hans", "en"}, AutomaticSubtitles: true, DownloadDanmaku: true, DownloadThumbnail: true, DownloadInfoJSON: true}
	if _, err := s.Create(ctx, Input{Query: "12345", Policy: policy}); err != nil {
		t.Fatal(err)
	}
	item := video(0)
	item.Title = `标题/不能:做\路径`
	item.PlatformID = "BV1234567890"
	item.ID = "bilibili:" + item.PlatformID
	item.URL = "https://www.bilibili.com/video/" + item.PlatformID + "/"
	source, err := s.Get(ctx, "12345")
	if err != nil {
		t.Fatal(err)
	}
	source.Items = []content.Item{item}
	source.Pending = []string{item.ID}
	s.pump(ctx, &source)
	if d.started != 1 {
		t.Fatalf("admitted %d jobs", d.started)
	}
	job := d.jobs[0]
	wantDirectory := "B站收藏夹/自定义 收藏"
	wantOutput := "标题_不能_做_路径 [BV1234567890]"
	if job.Subdirectory != wantDirectory || job.OutputDirectory != wantOutput || job.MaxHeight != 720 {
		t.Fatalf("download hierarchy/quality = %q / %q / %d", job.Subdirectory, job.OutputDirectory, job.MaxHeight)
	}
	if job.Options.Container != "mkv" || !job.Options.AutomaticSubtitles || !job.Options.DownloadDanmaku || !job.Options.DownloadThumbnail || !job.Options.DownloadInfoJSON || strings.Join(job.Options.SubtitleLanguages, ",") != "en,zh-Hans" {
		t.Fatalf("folder policy did not reach download job: %+v", job.Options)
	}
	if got := videoDirectoryName(content.Item{PlatformID: "BVfallback", Title: "///"}); got != "B站视频 [BVfallback]" {
		t.Fatalf("unsafe title fallback = %q", got)
	}
}

func TestFavoriteDirectoryNameCanBeResetToDefault(t *testing.T) {
	s, _, _, _ := newFixture(t)
	ctx := context.Background()
	policy := DefaultPolicy()
	policy.DirectoryName = "别名"
	if _, err := s.Create(ctx, Input{Query: "12345", Policy: policy}); err != nil {
		t.Fatal(err)
	}
	if source := syncOnce(t, s, "12345"); source.Title != "测试收藏夹" {
		t.Fatalf("fixture title = %q", source.Title)
	}
	policy.DirectoryName = ""
	updated, err := s.Configure(ctx, "12345", policy)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Policy.DirectoryName != "测试收藏夹 (12345)" {
		t.Fatalf("reset directory = %q", updated.Policy.DirectoryName)
	}
}
func TestSharedCompletedDownloadRemainsTrackedWhenItsFileDisappears(t *testing.T) {
	s, _, remote, d := newFixture(t)
	item := video(0)
	remote.pages[0].Items = []content.Item{item}
	remote.pages[0].Entries = 1
	remote.pages[0].Folder.Count = 1
	p := DefaultPolicy()
	p.AutoDownload = true
	d.jobs = []downloads.Job{{Options: p.Download, ID: "manual-completed", URL: item.URL, Subdirectory: "B站收藏夹/" + folderDirectoryName("测试收藏夹", "12345"), MaxHeight: p.MaxHeight, State: "completed"}}
	d.filePath = "/fixture/existing.mp4"
	if _, err := s.Create(context.Background(), Input{Query: "12345", Policy: p}); err != nil {
		t.Fatal(err)
	}
	source := syncOnce(t, s, "12345")
	if transfer := source.Transfers[item.ID]; transfer.JobID != "manual-completed" || transfer.Attempts != 0 || d.started != 0 {
		t.Fatalf("existing file not adopted without consuming an attempt: %+v", source)
	}
	source = syncOnce(t, s, "12345")
	if d.started != 0 || len(source.Pending) != 0 {
		t.Fatal("existing shared file was downloaded again")
	}
	d.filePath = ""
	source = syncOnce(t, s, "12345")
	if d.started != 1 || source.Transfers[item.ID].Attempts != 1 {
		t.Fatalf("removed shared file was not queued once: %+v", source)
	}
	syncOnce(t, s, "12345")
	if d.started != 1 {
		t.Fatal("active recovery download was duplicated")
	}
}

func video(i int) content.Item {
	id := fmt.Sprintf("BVfav%07d", i)
	return content.Item{ID: "bilibili:" + id, Platform: "bilibili", PlatformID: id, Kind: "video", Title: "明确测试视频", URL: "https://www.bilibili.com/video/" + id + "/"}
}
func newFixture(t *testing.T) (*Service, *memoryRepo, *fixtureRemote, *fixtureDownloads) {
	t.Helper()
	repo := &memoryRepo{records: map[string]Source{}}
	items := []content.Item{}
	for i := 0; i < 6; i++ {
		items = append(items, video(i))
	}
	remote := &fixtureRemote{pages: []Page{{Folder: Folder{ID: "12345", Title: "测试收藏夹", Count: 6}, Items: items, Entries: 6}}}
	d := &fixtureDownloads{}
	s := New(repo, remote, d)
	s.pageDelay = 0
	t.Cleanup(s.Close)
	return s, repo, remote, d
}
func waitIdle(t *testing.T, s *Service, id string) Source {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		source, err := s.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if !source.Active {
			return source
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("sync fixture did not finish")
	return Source{}
}
func syncOnce(t *testing.T, s *Service, id string) Source {
	t.Helper()
	if _, err := s.Start(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	return waitIdle(t, s, id)
}

func TestSelectionDefaultsOptInAndDownloadBudget(t *testing.T) {
	s, _, remote, d := newFixture(t)
	ctx := context.Background()
	source, err := s.Create(ctx, Input{Query: "https://space.bilibili.com/123/favlist?fid=12345"})
	if err != nil || source.Policy.Enabled || source.Policy.AutoDownload || len(source.Items) != 0 || remote.calls != 0 || d.started != 0 {
		t.Fatal(source, err)
	}
	duplicate, err := s.Create(ctx, Input{Query: "12345", Policy: Policy{Enabled: true, AutoDownload: true, IntervalMinutes: 60, MaxPerRun: 3, MaxHeight: 720}})
	if err != nil || duplicate.Policy.Enabled {
		t.Fatal("duplicate silently enables downloads", duplicate, err)
	}
	policy := DefaultPolicy()
	policy.AutoDownload = true
	if _, err := s.Configure(ctx, "12345", policy); err != nil {
		t.Fatal(err)
	}
	first := syncOnce(t, s, "12345")
	if len(first.Items) != 6 || len(first.Pending) != 3 || d.started != 3 || first.LastSuccessAt == 0 {
		t.Fatal(first, d.started)
	}
	second := syncOnce(t, s, "12345")
	if len(second.Pending) != 0 || d.started != 6 {
		t.Fatal(second, d.started)
	}
	syncOnce(t, s, "12345")
	if d.started != 6 {
		t.Fatal("duplicate download", d.started)
	}
	for _, job := range d.jobs {
		if job.MaxHeight != 1080 {
			t.Fatal(job)
		}
	}
}
func TestPartialScanNeverReplacesSnapshotOrAdmitsDownloads(t *testing.T) {
	s, repo, remote, d := newFixture(t)
	ctx := context.Background()
	policy := DefaultPolicy()
	policy.AutoDownload = true
	if _, err := s.Create(ctx, Input{Query: "12345", Policy: policy}); err != nil {
		t.Fatal(err)
	}
	first := syncOnce(t, s, "12345")
	remote.mu.Lock()
	remote.pages = []Page{{Folder: Folder{Title: "改名不应生效", Count: 7}, Entries: 1, Items: []content.Item{video(10)}, HasMore: true}, {Folder: Folder{Title: "改名不应生效", Count: 7}, Entries: 6, Items: first.Items}}
	remote.failPage = 2
	remote.mu.Unlock()
	failed := syncOnce(t, s, "12345")
	if failed.Title != first.Title || failed.LastSuccessAt != first.LastSuccessAt || len(failed.Items) != 6 || d.started != 3 {
		t.Fatal(failed, d.started)
	}
	if failed.Message != "同步未完成，上次成功内容已保留。" {
		t.Fatal("leaked upstream failure", failed.Message)
	}
	remote.mu.Lock()
	remote.failPage = 0
	remote.pages = []Page{{Folder: Folder{Title: "snapshot fixture", Count: 6}, Entries: 6, Items: first.Items}}
	remote.mu.Unlock()
	repo.mu.Lock()
	repo.failSnapshot = true
	repo.mu.Unlock()
	syncOnce(t, s, "12345")
	if d.started != 3 {
		t.Fatal("download admitted before snapshot commit")
	}
}

func TestFailedSnapshotPreservesTransfersAndRetryBudget(t *testing.T) {
	s, repo, remote, d := newFixture(t)
	ctx := context.Background()
	policy := DefaultPolicy()
	policy.AutoDownload = true
	if _, err := s.Create(ctx, Input{Query: "12345", Policy: policy}); err != nil {
		t.Fatal(err)
	}
	first := syncOnce(t, s, "12345")
	// Each original admission now has a terminal record: one exhausted, one
	// cancelled by the user, and one eligible for a final automatic retry.
	d.mu.Lock()
	for i := range d.jobs {
		d.jobs[i].State = "failed"
		if i == 1 {
			d.jobs[i].State = "cancelled"
		}
	}
	d.mu.Unlock()
	first.Transfers[first.Items[0].ID] = Transfer{JobID: "1", Attempts: 3}
	first.Transfers[first.Items[2].ID] = Transfer{JobID: "3", Attempts: 2}
	if err := repo.PutFavoriteSource(ctx, first); err != nil {
		t.Fatal(err)
	}
	beforeItems, _ := json.Marshal(first.Items)
	beforePending, _ := json.Marshal(first.Pending)
	beforeTransfers, _ := json.Marshal(first.Transfers)
	remote.mu.Lock()
	remote.pages = []Page{{Folder: Folder{Title: "Empty remote fixture", Count: 0}, Items: []content.Item{}, Entries: 0}}
	remote.mu.Unlock()
	repo.mu.Lock()
	repo.failSnapshot = true
	repo.mu.Unlock()
	failed := syncOnce(t, s, "12345")
	afterItems, _ := json.Marshal(failed.Items)
	afterPending, _ := json.Marshal(failed.Pending)
	afterTransfers, _ := json.Marshal(failed.Transfers)
	if string(afterItems) != string(beforeItems) || string(afterPending) != string(beforePending) || string(afterTransfers) != string(beforeTransfers) || failed.LastSuccessAt != first.LastSuccessAt || failed.Title != first.Title {
		t.Fatalf("failed snapshot changed saved content or transfers: %+v", failed)
	}
	if d.started != 3 || failed.Message != "收藏缓存未能写入，未开始新的下载。" {
		t.Fatal("failed snapshot admitted downloads or hid failure", failed, d.started)
	}
	repo.mu.Lock()
	repo.failSnapshot = false
	repo.mu.Unlock()
	remote.mu.Lock()
	remote.pages = []Page{{Folder: Folder{Title: first.Title, Count: 6}, Items: first.Items, Entries: 6}}
	remote.mu.Unlock()
	recovered := syncOnce(t, s, "12345")
	if d.started != 6 || recovered.Transfers[first.Items[0].ID].Attempts != 3 || recovered.Transfers[first.Items[1].ID].JobID != "2" || recovered.Transfers[first.Items[2].ID].Attempts != 2 {
		t.Fatal("restored snapshot lost exhausted/cancelled records", recovered, d.started)
	}
	// The three previously pending videos consume this round's admission budget.
	// The retry retains its original attempt count until the following round.
	recovered = syncOnce(t, s, "12345")
	if d.started != 7 || recovered.Transfers[first.Items[2].ID].Attempts != 3 {
		t.Fatal("eligible retry did not resume exactly once", recovered, d.started)
	}
	remote.mu.Lock()
	remote.pages = []Page{{Folder: Folder{Title: "Confirmed empty fixture", Count: 0}, Items: []content.Item{}, Entries: 0}}
	remote.mu.Unlock()
	removed := syncOnce(t, s, "12345")
	if len(removed.Items) != 0 || len(removed.Pending) != 0 || len(removed.Transfers) != 0 || d.started != 7 {
		t.Fatal("confirmed removal did not clean up tracking", removed, d.started)
	}
}

func TestImpossiblePageCountsStopBeforeFurtherRequests(t *testing.T) {
	for _, tc := range []struct {
		name string
		page Page
	}{
		{"more entries than declared", Page{Folder: Folder{Title: "invalid count", Count: 1}, Entries: 2, Items: []content.Item{video(10), video(11)}, HasMore: true}},
		{"excluded exceeds entries", Page{Folder: Folder{Title: "invalid exclusions", Count: 1}, Entries: 1, Excluded: 2, HasMore: true}},
		{"overflowing exclusions", Page{Folder: Folder{Title: "overflow exclusions", Count: 1}, Entries: 1, Excluded: int(^uint(0) >> 1), Items: []content.Item{video(10), video(11)}, HasMore: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, remote, d := newFixture(t)
			policy := DefaultPolicy()
			policy.AutoDownload = true
			if _, err := s.Create(context.Background(), Input{Query: "12345", Policy: policy}); err != nil {
				t.Fatal(err)
			}
			before := syncOnce(t, s, "12345")
			remote.mu.Lock()
			calls := remote.calls
			remote.pages = []Page{tc.page}
			remote.mu.Unlock()
			after := syncOnce(t, s, "12345")
			remote.mu.Lock()
			requests := remote.calls - calls
			remote.mu.Unlock()
			if requests != 1 || d.started != 3 || after.LastSuccessAt != before.LastSuccessAt || len(after.Items) != len(before.Items) || len(after.Pending) != len(before.Pending) {
				t.Fatalf("invalid snapshot changed state: requests=%d downloads=%d source=%+v", requests, d.started, after)
			}
		})
	}
}
func TestScheduleAndCrossFolderDownloads(t *testing.T) {
	s, _, remote, d := newFixture(t)
	ctx := context.Background()
	p := DefaultPolicy()
	p.Enabled = true
	p.AutoDownload = true
	p.MaxPerRun = 5
	remote.pages[0].Items = remote.pages[0].Items[:2]
	remote.pages[0].Entries = 2
	remote.pages[0].Folder.Count = 2
	s.Create(ctx, Input{Query: "12345", Policy: p})
	s.tick(time.Now())
	first := waitIdle(t, s, "12345")
	if d.started != 2 || first.LastSuccessAt == 0 {
		t.Fatal(first)
	}
	s.tick(time.Now())
	waitIdle(t, s, "12345")
	if remote.calls != 1 {
		t.Fatal("interval ignored")
	}
	s.Create(ctx, Input{Query: "23456", Policy: p})
	syncOnce(t, s, "23456")
	if d.started != 4 {
		t.Fatal("each folder should receive an isolated download", d.started)
	}
	p.Enabled = false
	s.Configure(ctx, "12345", p)
	count := remote.calls
	s.tick(time.Now().Add(24 * time.Hour))
	waitIdle(t, s, "23456")
	// Only the still-enabled second source can be refreshed.
	if remote.calls != count+1 {
		t.Fatal("disabled source scheduled", remote.calls, count)
	}
	if err := s.Remove(ctx, "12345"); err != nil {
		t.Fatal(err)
	}
	if d.started != 4 || len(d.jobs) != 4 {
		t.Fatal("removal affected downloads")
	}
}

func TestCrossFolderCancellationDoesNotAdmitAnotherDownload(t *testing.T) {
	s, _, remote, d := newFixture(t)
	ctx := context.Background()
	p := DefaultPolicy()
	p.AutoDownload = true
	remote.pages[0].Items = remote.pages[0].Items[:1]
	remote.pages[0].Entries = 1
	remote.pages[0].Folder.Count = 1
	if _, err := s.Create(ctx, Input{Query: "12345", Policy: p}); err != nil {
		t.Fatal(err)
	}
	syncOnce(t, s, "12345")
	d.mu.Lock()
	d.jobs[0].State = "cancelled"
	d.mu.Unlock()
	if _, err := s.Create(ctx, Input{Query: "23456", Policy: p}); err != nil {
		t.Fatal(err)
	}
	second := syncOnce(t, s, "23456")
	if d.started != 2 || len(second.Pending) != 0 || second.Transfers[video(0).ID].JobID != "2" {
		t.Fatal("folder-scoped transfer not admitted", second, d.started)
	}
	// A newer explicit queued job may still be shared rather than cancelled.
	job, err := d.Start(ctx, downloads.Request{URL: video(0).URL, Title: "explicit fixture retry", MaxHeight: 1080})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, Input{Query: "34567", Policy: p}); err != nil {
		t.Fatal(err)
	}
	third := syncOnce(t, s, "34567")
	if d.started != 3 || third.Transfers[video(0).ID].JobID != job.ID {
		t.Fatal("active manual download not adopted", third, d.started)
	}
}

func TestMalformedPagesAndFolderInputBoundaries(t *testing.T) {
	for _, bad := range []string{"0", "-1", "../id", "https://evil.example/?fid=12345", "https://space.bilibili.com.evil.example/?fid=12345", "http://space.bilibili.com/?fid=12345", "https://u:p@space.bilibili.com/?fid=12345"} {
		if _, err := ParseFolder(bad); !errors.Is(err, ErrInvalid) {
			t.Fatal(bad, err)
		}
	}
	s, _, remote, d := newFixture(t)
	ctx := context.Background()
	s.Create(ctx, Input{Query: "12345"})
	for _, page := range []Page{{Folder: Folder{Title: "invalid", Count: 1}, HasMore: true}, {Folder: Folder{Title: "invalid", Count: 1}, Items: []content.Item{video(1)}, Entries: 1, HasMore: true}, {Folder: Folder{Title: "invalid", Count: 1}, Items: []content.Item{video(1)}, Entries: 2}, {Folder: Folder{Title: "invalid", Count: 4001}, Items: []content.Item{}, Entries: 0}} {
		remote.mu.Lock()
		remote.pages = []Page{page}
		remote.mu.Unlock()
		// A repeat page is returned for all pages for this malformed fixture.
		if page.HasMore && page.Entries > 0 {
			remote.mu.Lock()
			remote.pages = append(remote.pages, page)
			remote.mu.Unlock()
		}
		result := syncOnce(t, s, "12345")
		if result.LastSuccessAt != 0 || d.started != 0 {
			t.Fatal(result)
		}
	}
}

func TestSchedulePrioritizesOldestAttemptAndDoesNotStarveNewFolders(t *testing.T) {
	s, repo, _, d := newFixture(t)
	ctx := context.Background()
	p := DefaultPolicy()
	p.Enabled = true
	now := time.Now()
	for _, id := range []string{"12345", "23456", "34567"} {
		if _, err := s.Create(ctx, Input{Query: id, Policy: p}); err != nil {
			t.Fatal(err)
		}
	}
	for id, age := range map[string]time.Duration{"12345": 2 * time.Hour, "23456": 3 * time.Hour} {
		source, _ := repo.GetFavoriteSource(ctx, id)
		source.LastAttemptAt = now.Add(-age).Unix()
		if err := repo.PutFavoriteSource(ctx, source); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"34567", "23456", "12345"} {
		s.tick(now)
		result := waitIdle(t, s, id)
		if result.LastSuccessAt == 0 {
			t.Fatalf("overdue folder %s did not receive its turn: %+v", id, result)
		}
	}
	if d.started != 0 {
		t.Fatal("scheduling enabled downloads without consent")
	}
}
