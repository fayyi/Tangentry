package sqlite

import (
	"context"
	"testing"
)

func TestDiscoveryBatchSnapshotIsScopedAndExpires(t *testing.T) {
	store, err := Open(t.TempDir() + "/batch.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.PutDiscoveryBatch(ctx, "batch-1", "request-a", []byte(`{"items":[]}`), 200); err != nil {
		t.Fatal(err)
	}
	if payload, err := store.GetDiscoveryBatch(ctx, "batch-1", "request-a", 100); err != nil || string(payload) != `{"items":[]}` {
		t.Fatalf("snapshot missing: %s %v", payload, err)
	}
	if _, err := store.GetDiscoveryBatch(ctx, "batch-1", "request-b", 100); err == nil {
		t.Fatal("snapshot crossed request scope")
	}
	if _, err := store.GetDiscoveryBatch(ctx, "batch-1", "request-a", 200); err == nil {
		t.Fatal("expired snapshot was returned")
	}
}
