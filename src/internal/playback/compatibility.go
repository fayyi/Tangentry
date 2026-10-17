package playback

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const maxCompatibilityInput = 256 << 20
const MaxCompatibilityOutput = 384 << 20

type CompatibilityJob struct {
	ID      string `json:"id"`
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
}

type compatibilityEntry struct {
	job     CompatibilityJob
	dir     string
	expires time.Time
	cancel  context.CancelFunc
}

// Compatibility keeps converted media outside the library and download queue.
// Only the resolver may supply media; requests never supply a CDN URL or path.
type Compatibility struct {
	mu      sync.Mutex
	jobs    map[string]*compatibilityEntry
	root    string
	http    *http.Client
	resolve func(context.Context, string) (Source, error)
	convert func(context.Context, string, string) error
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	closed  bool
	running bool
}

func compatibilityURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && len(raw) <= 8192 && u.Scheme == "https" && u.User == nil &&
		u.Port() == "" && strings.HasSuffix(strings.ToLower(u.Hostname()), ".xhscdn.com")
}

func NewCompatibility(client *http.Client, resolve func(context.Context, string) (Source, error), convert func(context.Context, string, string) error) (*Compatibility, error) {
	root, err := os.MkdirTemp("", "tangentry-playback-")
	if err != nil {
		return nil, err
	}
	clone := *client
	clone.Jar = nil
	clone.Timeout = 0
	clone.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !compatibilityURL(r.URL.String()) {
			return ErrUnavailable
		}
		r.Header.Del("Cookie")
		r.Header.Del("Authorization")
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Compatibility{jobs: make(map[string]*compatibilityEntry), root: root, http: &clone, resolve: resolve, convert: convert, ctx: ctx, cancel: cancel}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		timer := time.NewTicker(time.Minute)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-timer.C:
				s.mu.Lock()
				for id, entry := range s.jobs {
					if now.After(entry.expires) {
						entry.cancel()
						delete(s.jobs, id)
						_ = os.RemoveAll(entry.dir)
					}
				}
				s.mu.Unlock()
			}
		}
	}()
	return s, nil
}

func (s *Compatibility) Open(locator string) (CompatibilityJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return CompatibilityJob{}, ErrUnavailable
	}
	if s.running || len(s.jobs) >= 8 {
		return CompatibilityJob{}, ErrBusy
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return CompatibilityJob{}, ErrUnavailable
	}
	id := hex.EncodeToString(token[:])
	dir, err := os.MkdirTemp(s.root, "session-")
	if err != nil {
		return CompatibilityJob{}, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(s.ctx, 15*time.Minute)
	entry := &compatibilityEntry{job: CompatibilityJob{ID: id, State: "fetching"}, dir: dir, expires: time.Now().Add(2 * time.Hour), cancel: cancel}
	s.jobs[id] = entry
	s.running = true
	s.wg.Add(1)
	go s.run(ctx, entry, locator)
	return entry.job, nil
}

func (s *Compatibility) update(entry *compatibilityEntry, state, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry.job.State, entry.job.Message = state, message
}

func (s *Compatibility) run(ctx context.Context, entry *compatibilityEntry, locator string) {
	defer s.wg.Done()
	defer entry.cancel()
	ready := false
	defer func() {
		s.mu.Lock()
		s.running = false
		// Release can race with completion; a released session must not retain files.
		keep := ready && s.jobs[entry.job.ID] == entry && ctx.Err() == nil
		s.mu.Unlock()
		if !keep {
			_ = os.RemoveAll(entry.dir)
		}
	}()
	source, err := s.resolve(ctx, locator)
	if err != nil || !compatibilityURL(source.URL) || source.Duration > 1800 {
		s.update(entry, "failed", "未能取得适合兼容播放的视频，请刷新笔记后重试。")
		return
	}
	input := filepath.Join(entry.dir, "input.mp4")
	if err := s.fetch(ctx, source.URL, input); err != nil {
		s.update(entry, "failed", "视频读取失败或超过 256 MB，请稍后重试。")
		return
	}
	s.update(entry, "converting", "")
	output := filepath.Join(entry.dir, "playback.mp4")
	if s.convert(ctx, input, output) != nil {
		s.update(entry, "failed", "视频兼容处理未完成，请检查 FFmpeg 后重试。")
		return
	}
	info, err := os.Stat(output)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() >= MaxCompatibilityOutput-(1<<20) || ctx.Err() != nil {
		s.update(entry, "failed", "未能生成完整的兼容视频，请重试或打开原内容。")
		return
	}
	_ = os.Remove(input)
	ready = true
	s.update(entry, "ready", "")
}

func (s *Compatibility) fetch(ctx context.Context, raw, path string) error {
	r, _ := http.NewRequestWithContext(ctx, "GET", raw, nil)
	r.Header.Set("User-Agent", "Mozilla/5.0")
	r.Header.Set("Accept-Encoding", "identity")
	res, err := s.http.Do(r)
	if err != nil {
		return ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || res.ContentLength > maxCompatibilityInput {
		return ErrUnavailable
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return ErrUnavailable
	}
	n, copyErr := io.Copy(file, io.LimitReader(res.Body, maxCompatibilityInput+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || n == 0 || n > maxCompatibilityInput || res.ContentLength >= 0 && n != res.ContentLength {
		return ErrUnavailable
	}
	return nil
}

func (s *Compatibility) Get(id string) (CompatibilityJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.jobs[id]
	if entry == nil || time.Now().After(entry.expires) {
		return CompatibilityJob{}, ErrExpired
	}
	return entry.job, nil
}

func (s *Compatibility) File(id string) (*os.File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.jobs[id]
	if entry == nil || time.Now().After(entry.expires) {
		return nil, ErrExpired
	}
	if entry.job.State != "ready" {
		return nil, ErrUnavailable
	}
	file, err := os.Open(filepath.Join(entry.dir, "playback.mp4"))
	if err != nil {
		return nil, ErrUnavailable
	}
	return file, nil
}

func (s *Compatibility) Release(id string) {
	s.mu.Lock()
	entry := s.jobs[id]
	delete(s.jobs, id)
	if entry != nil {
		entry.cancel()
	}
	s.mu.Unlock()
	if entry != nil {
		_ = os.RemoveAll(entry.dir)
	}
}

func (s *Compatibility) Close() error {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	s.wg.Wait()
	err := os.RemoveAll(s.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
