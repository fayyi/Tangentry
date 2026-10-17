package transcription

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fixtureInstaller struct {
	mu        sync.Mutex
	calls     int
	installed bool
	started   chan struct{}
	release   chan struct{}
}

func (f *fixtureInstaller) Available(string) bool { return true }
func (f *fixtureInstaller) Installed(ModelOption) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return "/fixture/model", f.installed
}
func (f *fixtureInstaller) Install(ctx context.Context, option ModelOption, python string, stage func(string)) error {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	close(f.started)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-f.release:
	}
	stage("verifying")
	f.mu.Lock()
	f.installed = true
	f.mu.Unlock()
	return nil
}
func TestModelDownloadRequiresExplicitSelectionAndNeverChangesConfiguration(t *testing.T) {
	f := &fixtureInstaller{started: make(chan struct{}), release: make(chan struct{})}
	s := NewModelDownloads(f, func() string { return "fixture-python" })
	defer s.Close()
	models, job, _ := s.Status()
	if len(models) != len(ModelCatalog()) || job != nil || f.calls != 0 {
		t.Fatal("reading catalog started download")
	}
	if _, err := s.Start("../../arbitrary"); err != ErrInvalid {
		t.Fatal("unknown model accepted", err)
	}
	if _, err := s.Start("large-v3-turbo"); err != nil {
		t.Fatal(err)
	}
	<-f.started
	if _, err := s.Start("small"); err != ErrBusy {
		t.Fatal("concurrent download accepted", err)
	}
	close(f.release)
	s.wait.Wait()
	_, job, _ = s.Status()
	if job.State != "completed" || f.calls != 1 {
		t.Fatal(job, f.calls)
	}
	if _, err := s.Start("large-v3-turbo"); err != nil || f.calls != 1 {
		t.Fatal("installed model downloaded again", err)
	}
}
func TestCancelModelDownloadDoesNotPublishModel(t *testing.T) {
	f := &fixtureInstaller{started: make(chan struct{}), release: make(chan struct{})}
	s := NewModelDownloads(f, func() string { return "fixture-python" })
	defer s.Close()
	job, err := s.Start("medium")
	if err != nil {
		t.Fatal(err)
	}
	<-f.started
	if err = s.Cancel("wrong-job"); err != ErrNotFound {
		t.Fatal(err)
	}
	if err = s.Cancel(job.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { s.wait.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancellation blocked")
	}
	_, current, _ := s.Status()
	if current.State != "canceled" || f.installed {
		t.Fatal(current)
	}
}

type slowCancelInstaller struct {
	started chan struct{}
	staged  chan struct{}
	release chan struct{}
}

func (f *slowCancelInstaller) Available(string) bool { return true }
func (f *slowCancelInstaller) Installed(ModelOption) (string, bool) {
	return "", false
}
func (f *slowCancelInstaller) Install(ctx context.Context, _ ModelOption, _ string, stage func(string)) error {
	close(f.started)
	<-ctx.Done()
	stage("verifying")
	close(f.staged)
	<-f.release
	return ctx.Err()
}

func TestModelCancelRemainsVisibleAndBusyUntilInstallerExits(t *testing.T) {
	f := &slowCancelInstaller{started: make(chan struct{}), staged: make(chan struct{}), release: make(chan struct{})}
	s := NewModelDownloads(f, func() string { return "fixture-python" })
	defer s.Close()
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(f.release) }) }
	defer release()
	job, err := s.Start("small")
	if err != nil {
		t.Fatal(err)
	}
	<-f.started
	if err := s.Cancel(job.ID); err != nil {
		t.Fatal(err)
	}
	<-f.staged
	_, current, _ := s.Status()
	if current.State != "cancelling" {
		t.Fatal("late installer stage hid cancellation", current)
	}
	if err := s.Cancel(job.ID); err != nil {
		t.Fatal("repeated cancel not idempotent", err)
	}
	if _, err := s.Start("base"); err != ErrBusy {
		t.Fatal("new download accepted before installer exit", err)
	}
	release()
	s.wait.Wait()
	_, current, _ = s.Status()
	if current.State != "canceled" {
		t.Fatal("terminal cancellation not recorded", current)
	}
}
