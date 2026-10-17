package tools

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

// ManagedState is what settings show about a downloaded tool.
type ManagedState struct {
	Version     string
	Installed   bool
	Downloading bool
	Received    int64
	Total       int64
	Error       string
}

type managedManifest struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
}

// managedTool keeps one verified executable in the app data directory. Only
// one download runs at a time; it is detached from requests waiting on it, so
// a canceled page load does not discard a partly finished download.
type managedTool struct {
	name    string
	dir     string
	client  *http.Client
	pinned  func() (managedRelease, error)
	mu      sync.Mutex
	running chan struct{}
	result  error
	state   ManagedState
}

func newManagedTool(name, dir string, pinned func() (managedRelease, error)) *managedTool {
	m := &managedTool{name: name, dir: dir, client: &http.Client{}, pinned: pinned}
	// Staging files of an interrupted download are never used; remove them.
	if matches, err := filepath.Glob(filepath.Join(dir, "."+name+"-download-*")); err == nil {
		for _, stale := range matches {
			_ = os.RemoveAll(stale)
		}
	}
	return m
}

func (m *managedTool) path() string { return filepath.Join(m.dir, exeName(m.name)) }

func (m *managedTool) manifestPath() string { return filepath.Join(m.dir, m.name+".json") }

// installed reports the verified version on disk; the manifest is written
// only after the executable was verified and published.
func (m *managedTool) installed() (string, bool) {
	data, err := os.ReadFile(m.manifestPath())
	if err != nil {
		return "", false
	}
	var manifest managedManifest
	if json.Unmarshal(data, &manifest) != nil || manifest.Version == "" || !sha256Pattern.MatchString(manifest.SHA256) || manifest.Size <= 0 {
		return "", false
	}
	info, err := os.Stat(m.path())
	if err != nil || !info.Mode().IsRegular() || info.Size() != manifest.Size {
		return "", false
	}
	if digest, err := sha256File(m.path()); err != nil || digest != manifest.SHA256 {
		return "", false
	}
	return manifest.Version, true
}

func (m *managedTool) snapshot() ManagedState {
	m.mu.Lock()
	state := m.state
	m.mu.Unlock()
	state.Version, state.Installed = m.installed()
	return state
}

// ensure returns the executable, downloading the pinned release on first use.
func (m *managedTool) ensure(ctx context.Context) (string, error) {
	if _, ok := m.installed(); ok {
		return m.path(), nil
	}
	done := m.begin(func(context.Context) (managedRelease, error) { return m.pinned() }, false)
	select {
	case <-done:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if _, ok := m.installed(); ok {
		return m.path(), nil
	}
	m.mu.Lock()
	err := m.result
	m.mu.Unlock()
	if err == nil {
		err = errors.New(m.name + " is not installed")
	}
	return "", err
}

var errManagedBusy = errors.New("managed tool download in progress")

// update replaces the installed executable with a newer verified release.
func (m *managedTool) update(resolve func(context.Context) (managedRelease, error)) error {
	m.mu.Lock()
	busy := m.running != nil
	m.mu.Unlock()
	if busy {
		return errManagedBusy
	}
	m.begin(resolve, true)
	return nil
}

func (m *managedTool) begin(resolve func(context.Context) (managedRelease, error), replace bool) chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running != nil {
		return m.running
	}
	done := make(chan struct{})
	m.running = done
	m.state = ManagedState{Downloading: true}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), managedTimeout)
		defer cancel()
		err := m.install(ctx, resolve, replace)
		if err != nil {
			slog.Warn("managed tool download failed", "tool", m.name, "error", err)
		}
		m.mu.Lock()
		m.running, m.result = nil, err
		m.state = ManagedState{}
		if err != nil {
			m.state.Error = m.name + " 未能完整下载，请检查网络后重试。"
		}
		m.mu.Unlock()
		close(done)
	}()
	return done
}

func (m *managedTool) progress(received, total int64) {
	m.mu.Lock()
	m.state.Received, m.state.Total = received, total
	m.mu.Unlock()
}

func (m *managedTool) install(ctx context.Context, resolve func(context.Context) (managedRelease, error), replace bool) error {
	if _, ok := m.installed(); ok && !replace {
		return nil
	}
	release, err := resolve(ctx)
	if err != nil {
		return err
	}
	if version, ok := m.installed(); ok && version == release.Version {
		return nil
	}
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(m.dir, "."+m.name+"-download-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	downloaded := filepath.Join(staging, "download")
	if err := m.download(ctx, release, downloaded); err != nil {
		return err
	}
	executable := downloaded
	if release.Member != "" {
		executable = filepath.Join(staging, exeName(m.name))
		if err := extractMember(downloaded, release.Member, executable); err != nil {
			return err
		}
	}
	info, err := os.Stat(executable)
	if err != nil {
		return err
	}
	if err := os.Chmod(executable, 0755); err != nil {
		return err
	}
	digest, err := sha256File(executable)
	if err != nil {
		return err
	}
	// Publish the executable before its manifest: a crash in between leaves
	// a file that installed() does not trust and the next use replaces.
	_ = os.Remove(m.manifestPath())
	if err := os.Rename(executable, m.path()); err != nil {
		return err
	}
	manifest, _ := json.Marshal(managedManifest{Version: release.Version, SHA256: digest, Size: info.Size()})
	temporary := filepath.Join(staging, "manifest.json")
	if err := os.WriteFile(temporary, manifest, 0600); err != nil {
		return err
	}
	return os.Rename(temporary, m.manifestPath())
}
