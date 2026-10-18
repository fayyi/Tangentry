package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

type updaterStatus struct {
	Enabled          bool    `json:"enabled"`
	CurrentVersion   string  `json:"currentVersion"`
	Available        bool    `json:"available"`
	Version          string  `json:"version,omitempty"`
	Notes            string  `json:"notes,omitempty"`
	Date             string  `json:"date,omitempty"`
	State            string  `json:"state"`
	Progress         float64 `json:"progress"`
	Error            string  `json:"error,omitempty"`
	AutomaticChecks  bool    `json:"automaticChecks"`
	LastCheck        string  `json:"lastCheck,omitempty"`
	SkippedVersion   string  `json:"skippedVersion,omitempty"`
	RelaunchRequired bool    `json:"relaunchRequired"`
}

type updaterPreferences struct {
	AutomaticChecks bool      `json:"automaticChecks"`
	SkippedVersion  string    `json:"skippedVersion,omitempty"`
	LastCheck       time.Time `json:"lastCheck,omitempty"`
}

var updaterStatusChanged = mygo.NewEvent[updaterStatus]("tangentry-updater")

type updaterService struct {
	mu        sync.Mutex
	operation sync.Mutex
	dataPath  string
	prefs     updaterPreferences
	status    updaterStatus
	update    *mygo.Update
	check     func(context.Context) (*mygo.Update, error)
	install   func(context.Context, *mygo.Update, func(int64, int64)) error
}

func newUpdaterService(dataDir string) *updaterService {
	prefs := loadUpdaterPreferences(filepath.Join(dataDir, "updater.json"))
	return &updaterService{
		dataPath: filepath.Join(dataDir, "updater.json"),
		prefs:    prefs,
		status: updaterStatus{
			Enabled:         mygo.Updater.Enabled(),
			CurrentVersion:  mygo.App.Version(),
			State:           "idle",
			AutomaticChecks: prefs.AutomaticChecks,
			SkippedVersion:  prefs.SkippedVersion,
			LastCheck:       formatUpdaterTime(prefs.LastCheck),
		},
		check: mygo.Updater.Check,
		install: func(ctx context.Context, update *mygo.Update, progress func(int64, int64)) error {
			return update.Install(ctx, progress)
		},
	}
}

func loadUpdaterPreferences(path string) updaterPreferences {
	defaults := updaterPreferences{AutomaticChecks: true}
	data, err := os.ReadFile(path)
	if err == nil {
		prefs := defaults
		if unmarshalErr := json.Unmarshal(data, &prefs); unmarshalErr == nil {
			return prefs
		} else {
			slog.Warn("无法读取自动更新设置，使用默认值", "error", unmarshalErr)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		slog.Warn("无法读取自动更新设置，使用默认值", "error", err)
	}
	return defaults
}

func (s *updaterService) savePreferencesLocked() error {
	data, err := json.MarshalIndent(s.prefs, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.dataPath, append(data, '\n'), 0600); err != nil {
		return fmt.Errorf("保存自动更新设置：%w", err)
	}
	return nil
}

func formatUpdaterTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func (s *updaterService) statusLocked() updaterStatus {
	status := s.status
	status.AutomaticChecks = s.prefs.AutomaticChecks
	status.SkippedVersion = s.prefs.SkippedVersion
	status.LastCheck = formatUpdaterTime(s.prefs.LastCheck)
	return status
}

func (s *updaterService) broadcast() {
	s.mu.Lock()
	status := s.statusLocked()
	s.mu.Unlock()
	_ = updaterStatusChanged.Broadcast(status)
}

func (s *updaterService) Status() updaterStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked()
}

// Check performs a signed manifest check. Manual checks show a skipped version;
// background checks leave it hidden until a newer release appears.
func (s *updaterService) Check(ctx context.Context, manual bool) (updaterStatus, error) {
	s.operation.Lock()
	defer s.operation.Unlock()
	s.mu.Lock()
	s.status = s.statusLocked()
	s.status.State, s.status.Error = "checking", ""
	s.status.Progress, s.status.RelaunchRequired = 0, false
	s.mu.Unlock()
	s.broadcast()

	checker := s.check
	if checker == nil {
		checker = mygo.Updater.Check
	}
	update, err := checker(ctx)
	s.mu.Lock()
	s.prefs.LastCheck = time.Now().UTC()
	if saveErr := s.savePreferencesLocked(); err == nil && saveErr != nil {
		err = saveErr
	}
	s.update = update
	s.status.Enabled = mygo.Updater.Enabled()
	s.status.CurrentVersion = mygo.App.Version()
	s.status.LastCheck = formatUpdaterTime(s.prefs.LastCheck)
	if err != nil {
		s.status.Available, s.status.State, s.status.Error = false, "error", formatUpdaterError(err)
	} else if update == nil || (!manual && updaterShouldSkip(update.Version, s.prefs.SkippedVersion)) {
		s.status.Available, s.status.Version, s.status.Notes, s.status.Date = false, "", "", ""
		s.status.State = "idle"
	} else {
		s.status.Available, s.status.Version, s.status.Notes = true, update.Version, update.Notes
		s.status.Date, s.status.State = formatUpdaterTime(update.Date), "available"
	}
	status := s.statusLocked()
	s.mu.Unlock()
	s.broadcast()
	return status, err
}

func formatUpdaterError(err error) string {
	if errors.Is(err, mygo.ErrUpdatesDisabled) {
		return "此版本未启用自动更新，请安装正式发布版本。"
	}
	if strings.Contains(err.Error(), "not signed with the app's key") {
		return "更新签名校验失败，已取消安装。"
	}
	if strings.Contains(err.Error(), "cannot write next to the app") {
		return "无法写入应用安装目录，请将应用移到当前用户可写的位置后重试。"
	}
	return err.Error()
}

func (s *updaterService) SetAutomaticChecks(enabled bool) (updaterStatus, error) {
	s.mu.Lock()
	s.prefs.AutomaticChecks = enabled
	err := s.savePreferencesLocked()
	status := s.statusLocked()
	s.mu.Unlock()
	if err == nil {
		s.broadcast()
	}
	return status, err
}

func (s *updaterService) Skip(version string) (updaterStatus, error) {
	if version == "" {
		return s.Status(), errors.New("更新版本为空")
	}
	s.mu.Lock()
	s.prefs.SkippedVersion = version
	err := s.savePreferencesLocked()
	if err == nil && s.status.Version == version {
		s.status.Available, s.status.Version, s.status.Notes, s.status.Date = false, "", "", ""
		s.status.State = "idle"
	}
	status := s.statusLocked()
	s.mu.Unlock()
	if err == nil {
		s.broadcast()
	}
	return status, err
}

func (s *updaterService) Install(ctx context.Context) (updaterStatus, error) {
	s.operation.Lock()
	defer s.operation.Unlock()
	s.mu.Lock()
	update := s.update
	if update == nil || !s.status.Available {
		s.mu.Unlock()
		return s.Status(), errors.New("没有可安装的更新")
	}
	s.status.State, s.status.Error, s.status.Progress = "downloading", "", 0
	s.mu.Unlock()
	s.broadcast()
	installer := s.install
	if installer == nil {
		installer = func(ctx context.Context, update *mygo.Update, progress func(int64, int64)) error {
			return update.Install(ctx, progress)
		}
	}
	err := installer(ctx, update, func(downloaded, total int64) {
		progress := 0.0
		if total > 0 {
			progress = float64(downloaded) / float64(total)
		}
		s.mu.Lock()
		s.status.Progress = progress
		s.mu.Unlock()
		s.broadcast()
	})
	s.mu.Lock()
	if err != nil {
		// Keep the discovered release visible so the settings page and the
		// non-blocking notice can explain why installation was refused.
		s.status.Available = true
		s.status.State, s.status.Error, s.status.Progress = "error", formatUpdaterError(err), 0
	} else {
		s.status.State, s.status.Error, s.status.Progress = "installed", "", 1
		s.status.RelaunchRequired = true
	}
	status := s.statusLocked()
	s.mu.Unlock()
	s.broadcast()
	return status, err
}

func (s *updaterService) Relaunch() (updaterStatus, error) {
	s.mu.Lock()
	ready := s.status.RelaunchRequired
	status := s.statusLocked()
	s.mu.Unlock()
	if !ready {
		return status, errors.New("更新尚未安装")
	}
	mygo.App.Relaunch()
	return status, nil
}

func (s *updaterService) StartBackground() {
	go func() {
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		firstCheck := true
		for {
			<-timer.C
			s.mu.Lock()
			due := s.prefs.AutomaticChecks && (firstCheck || updaterCheckDue(s.prefs.LastCheck, time.Now(), updaterInterval))
			s.mu.Unlock()
			if due {
				_, _ = s.Check(context.Background(), false)
			}
			firstCheck = false
			timer.Reset(updaterInterval)
		}
	}()
}
