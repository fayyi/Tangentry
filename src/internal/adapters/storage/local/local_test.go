package local

import (
	"context"
	"io"
	"testing"
)

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

type fixtureReader struct{}

func (*fixtureReader) Read(p []byte) (int, error) { copy(p, "hello"); return 5, io.EOF }
