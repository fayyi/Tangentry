package readstate

import (
	"context"
	"strings"
	"testing"
)

type recorder struct {
	ids   []string
	calls int
}

func (r *recorder) ListReadStates(_ context.Context, ids []string) ([]State, error) {
	r.calls++
	r.ids = ids
	return []State{}, nil
}
func (r *recorder) SetReadStates(_ context.Context, ids []string, _ bool, _ int64) ([]State, error) {
	r.calls++
	r.ids = ids
	return []State{}, nil
}

func TestBatchValidatesEveryIdentityBeforeAccess(t *testing.T) {
	r := &recorder{}
	s := New(r)
	ctx := context.Background()
	valid := []string{"bilibili:BV17x411w7KC", "youtube:dQw4w9WgXcQ", "rss:" + strings.Repeat("a", 64), "pdf:" + strings.Repeat("b", 64), "xiaoyuzhou:" + strings.Repeat("c", 24)}
	for _, id := range valid {
		if _, err := s.Set(ctx, []string{id, id}, true); err != nil || len(r.ids) != 1 {
			t.Fatalf("valid identity %s: %v", id, err)
		}
	}
	before := r.calls
	cases := [][]string{nil, {}, {valid[0], "invalid"}, {"bilibili:creator:12345"}, {"memo:" + strings.Repeat("a", 32)}, {"rss:feed:" + strings.Repeat("a", 64)}, {"rss:" + strings.Repeat("A", 64)}}
	over := make([]string, MaxBatch+1)
	for i := range over {
		over[i] = valid[0]
	}
	cases = append(cases, over)
	for _, ids := range cases {
		if _, err := s.Batch(ctx, ids); err != ErrInvalid {
			t.Fatal("batch accepted invalid input")
		}
		if _, err := s.Set(ctx, ids, false); err != ErrInvalid {
			t.Fatal("write accepted invalid input")
		}
	}
	if r.calls != before {
		t.Fatal("invalid batch reached persistence")
	}
}
