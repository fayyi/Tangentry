package local

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewRejectsSymlinkRoot(t *testing.T) {
	outside := t.TempDir()
	link := filepath.Join(t.TempDir(), "target")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("symlink unavailable")
	}
	if _, err := New(link); err == nil {
		t.Fatalf("symlink root accepted: %v", err)
	}
}

func TestStoreUploadReadListDelete(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Upload(ctx, "tangentry/v1/snapshots/fixture.tgb", io.LimitReader(&fixtureReader{}, 5)); err != nil {
		t.Fatal(err)
	}
	r, err := s.Read(ctx, "tangentry/v1/snapshots/fixture.tgb")
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(r)
	r.Close()
	if err != nil || string(b) != "hello" {
		t.Fatalf("read %q: %v", b, err)
	}
	items, err := s.List(ctx, "tangentry/v1/snapshots")
	if err != nil || len(items) != 1 {
		t.Fatalf("list %#v: %v", items, err)
	}
	if err := s.Delete(ctx, items[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(ctx, items[0]); err == nil {
		t.Fatal("deleted object still readable")
	}
}

func TestStoreRejectsSymlinkObjects(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "tangentry", "v1", "snapshots"), 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "tangentry", "v1", "snapshots", "link.tgb")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("symlink unavailable")
	}
	if _, err := s.Read(context.Background(), "tangentry/v1/snapshots/link.tgb"); err == nil {
		t.Fatal("symlink object accepted")
	}
}

func TestStoreRejectsSymlinkParents(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skip("symlink unavailable")
	}
	if err := s.Upload(context.Background(), "escape/object.tgb", strings.NewReader("fixture")); err == nil {
		t.Fatal("symlink parent accepted")
	}
}

type fixtureReader struct{}

func (*fixtureReader) Read(p []byte) (int, error) { copy(p, "hello"); return 5, io.EOF }
