package favoritesync

import (
	"context"
	"errors"
	"testing"
	"time"
)

type waitingFolderRemote struct {
	Remote
	started chan context.Context
}

func (r *waitingFolderRemote) Folders(ctx context.Context) ([]Folder, error) {
	r.started <- ctx
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestRemoteFolderReadSharesGateAndServiceShutdownCancelsIt(t *testing.T) {
	repo := &memoryRepo{records: map[string]Source{}}
	remote := &waitingFolderRemote{started: make(chan context.Context, 1)}
	s := New(repo, remote, nil)
	t.Cleanup(s.Close)
	done := make(chan error, 1)
	go func() { _, err := s.Folders(context.Background()); done <- err }()
	var run context.Context
	select {
	case run = <-remote.started:
	case <-time.After(time.Second):
		t.Fatal("listing did not start")
	}
	deadline, ok := run.Deadline()
	if !ok || time.Until(deadline) > 30*time.Second {
		t.Fatal("unbounded remote listing")
	}
	if _, err := s.Folders(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatal("overlapping listing", err)
	}
	if _, err := s.Start(context.Background(), "12345"); !errors.Is(err, ErrBusy) {
		t.Fatal("sync overlapped listing", err)
	}
	if _, err := s.List(context.Background()); err != nil {
		t.Fatal("local reads blocked", err)
	}
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel listing")
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("shutdown cancellation lost", err)
	}
	if _, err := s.Folders(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatal("closed service contacted platform", err)
	}
}

func TestSyncGateBlocksFolderRequestsAndReleasesAfterCancellation(t *testing.T) {
	repo := &memoryRepo{records: map[string]Source{}}
	remote := &countedFolders{}
	s := New(repo, remote, nil)
	defer s.Close()
	release, err := s.begin("12345")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Folders(context.Background()); !errors.Is(err, ErrBusy) || remote.calls != 0 {
		t.Fatal("listing bypassed sync", err, remote.calls)
	}
	release()
	if _, err := s.Folders(context.Background()); err != nil || remote.calls != 1 {
		t.Fatal("released gate stuck", err, remote.calls)
	}
}
