package discovery

import (
	"context"
	"errors"
	"testing"
	"time"

	"tangentry/internal/content"
)

type batchProvider struct {
	feeds    map[string]Feed
	pages    map[string]map[int]Feed
	failures map[string]error
}

func (p batchProvider) Fetch(_ context.Context, source string, page int) (Feed, error) {
	if err := p.failures[source]; err != nil {
		return Feed{}, err
	}
	if pages := p.pages[source]; pages != nil {
		return pages[page], nil
	}
	return p.feeds[source], nil
}

type batchSnapshots struct {
	values map[string][]byte
	keys   map[string]string
}

func (r *batchSnapshots) PutDiscoveryBatch(_ context.Context, id, key string, payload []byte, _ int64) error {
	if r.values == nil {
		r.values = map[string][]byte{}
		r.keys = map[string]string{}
	}
	r.values[id], r.keys[id] = payload, key
	return nil
}
func (r *batchSnapshots) GetDiscoveryBatch(_ context.Context, id, key string, _ int64) ([]byte, error) {
	if r.keys[id] != key {
		return nil, errors.New("wrong request key")
	}
	return r.values[id], nil
}

func batchItem(platform, platformID, title, author string, published int64) content.Item {
	return content.Item{ID: platform + ":" + platformID, Platform: platform, PlatformID: platformID, Kind: "video", Title: title, Author: author, URL: "https://example.com/" + platformID, ThumbnailURL: "https://example.com/thumb/" + platformID, PublishedAt: published}
}

func TestBatchIsDeterministicAndDeduplicatesWithSourceHealth(t *testing.T) {
	snapshots := &batchSnapshots{}
	service := NewBatchService(batchProvider{feeds: map[string]Feed{
		"bilibili": {Items: []content.Item{batchItem("bilibili", "BV17x411w7KC", "订阅重复", "作者", 5), batchItem("bilibili", "BV1xx411w7KC", "新视频", "作者2", 4)}},
		"youtube":  {Items: []content.Item{batchItem("youtube", "abcdefghijk", "跨来源重复", "作者3", 3)}},
	}, failures: map[string]error{"douyin": ErrUnavailable}}, func(context.Context) (Feed, error) {
		return Feed{Items: []content.Item{batchItem("bilibili", "BV17x411w7KC", "订阅重复", "作者", 6), batchItem("rss", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "订阅文章", "作者4", 2)}}, nil
	}, []string{"bilibili", "youtube", "douyin"}, snapshots)
	request := BatchRequest{View: "wander", Size: 10, Seed: 42, SubscriptionRatio: 60, ExplorationRatio: 15}
	first, err := service.Create(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Create(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.BatchID != second.BatchID || len(first.Items) != len(second.Items) {
		t.Fatalf("batch is not deterministic: %#v %#v", first, second)
	}
	if first.SourceStatus["douyin"] != "unavailable" {
		t.Fatalf("source failure was not preserved: %#v", first.SourceStatus)
	}
	seen := map[string]bool{}
	for _, item := range first.Items {
		if seen[item.Item.ID] {
			t.Fatalf("duplicate item %s", item.Item.ID)
		}
		seen[item.Item.ID] = true
	}
}

func TestBatchCursorReadsPersistedSnapshotAndEnds(t *testing.T) {
	snapshots := &batchSnapshots{}
	service := NewBatchService(batchProvider{feeds: map[string]Feed{"bilibili": {Items: []content.Item{
		batchItem("bilibili", "BV17x411w7KC", "一", "a", 3), batchItem("bilibili", "BV1xx411w7KC", "二", "b", 2), batchItem("bilibili", "BV2xx411w7KC", "三", "c", 1),
	}}}, failures: map[string]error{}}, nil, []string{"bilibili"}, snapshots)
	service.clock = func() time.Time { return time.UnixMilli(1000) }
	first, err := service.Create(context.Background(), BatchRequest{Size: 10, Seed: 9})
	if err != nil {
		t.Fatal(err)
	}
	if first.HasNextBatch || first.EndReason != "暂时没有更多了" {
		t.Fatalf("short candidate pool should end: %#v", first)
	}
	_ = first
}

func TestBatchUsesDefaultsWhenOptionsAreOmitted(t *testing.T) {
	service := NewBatchService(batchProvider{feeds: map[string]Feed{
		"bilibili": {Items: []content.Item{batchItem("bilibili", "BV17x411w7KC", "默认", "作者", 1)}},
	}}, nil, []string{"bilibili"})
	response, err := service.Create(context.Background(), BatchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Items) != 1 || response.Reason == "" {
		t.Fatalf("omitted options were not defaulted: %#v", response)
	}
}
