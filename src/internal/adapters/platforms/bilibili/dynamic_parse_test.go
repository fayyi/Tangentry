package bilibili

import (
	"encoding/json"
	"strings"
	"testing"
)

// These fixtures intentionally contain only the small, stable subset used by
// the feed parser.  The real endpoint mixes numeric and string representations
// for IDs, timestamps and counters, so both forms are covered here.
func TestDynamicItemMapsVideoFieldsWithMixedJSONTypes(t *testing.T) {
	raw := json.RawMessage(`{
		"type":"DYNAMIC_TYPE_AV","id_str":"feed-av-1",
		"modules":{"module_author":{"mid":"42","name":"视频作者","face":"//i0.hdslb.com/avatar.jpg","pub_ts":"1700000000","pub_time":"刚刚"},
		"module_dynamic":{"desc":{"text":"动态文案"},"major":{"archive":{
			"aid":"123","bvid":"BV17x411w7KC","title":"视频标题","cover":"//i0.hdslb.com/cover.jpg",
			"desc":"视频简介","duration_text":"01:02","stat":{"play":"3.4万","view":1234}
		}}}}
	}`)
	item, ok := dynamicItem(raw, 0)
	if !ok {
		t.Fatalf("dynamicItem rejected mixed-type AV fixture: %#v", item)
	}
	if item.ID != "bilibili:BV17x411w7KC" || item.PlatformID != "BV17x411w7KC" || item.Kind != "video" {
		t.Fatalf("unexpected video identity: %#v", item)
	}
	if item.Title != "视频标题" || item.Author != "视频作者" || item.AuthorID != "42" || item.PublishedAt != 1700000000 || item.PublishedText != "刚刚" {
		t.Fatalf("video metadata lost: %#v", item)
	}
	if item.URL != "https://www.bilibili.com/video/BV17x411w7KC/" || item.ThumbnailURL != "https://i0.hdslb.com/cover.jpg" || item.DurationSeconds != 62 || item.ViewCount != 1234 {
		t.Fatalf("video fields not normalized: %#v", item)
	}
	if !strings.Contains(item.Description, "动态文案") || !strings.Contains(item.Description, "视频简介") {
		t.Fatalf("video description missing dynamic text: %#v", item.Description)
	}

	// A malformed optional field must not make the complete item disappear.
	malformed := json.RawMessage(`{
		"type":"DYNAMIC_TYPE_AV","id_str":"feed-av-2",
		"modules":{"module_author":{"mid":{"unexpected":true},"name":"仍可显示","pub_ts":[],"face":null},
		"module_dynamic":{"major":{"archive":{"bvid":"BV1Pyta66EDh","title":"字段异常仍可显示","duration_text":{},"stat":{"play":{}}}}}}
	}`)
	item, ok = dynamicItem(malformed, 0)
	if !ok || item.ID != "bilibili:BV1Pyta66EDh" || item.Title != "字段异常仍可显示" || item.DurationSeconds != 0 || item.ViewCount != 0 {
		t.Fatalf("optional field type mismatch dropped AV: ok=%t item=%#v", ok, item)
	}
}

func TestDynamicItemMapsForwardedVideoAndHandlesNullOriginal(t *testing.T) {
	original := `{"type":"DYNAMIC_TYPE_AV","id_str":"orig-av","modules":{"module_author":{"mid":7,"name":"原作者","pub_ts":1700000000},"module_dynamic":{"major":{"archive":{"bvid":"BV1Q541167Qg","title":"原视频","duration_text":"00:10","stat":{"play":9012}}}}}}`
	forward := json.RawMessage(`{"type":"DYNAMIC_TYPE_FORWARD","id_str":"forward-1","modules":{"module_author":{"mid":"99","name":"转发作者","pub_ts":"1700000010"},"module_dynamic":{"desc":{"text":"我的转发语"}}},"orig":` + original + `}`)
	item, ok := dynamicItem(forward, 0)
	if !ok || item.Kind != "video" || item.ID != "bilibili:BV1Q541167Qg" {
		t.Fatalf("forwarded video was not mapped: ok=%t item=%#v", ok, item)
	}
	if item.Author != "转发作者" || item.AuthorID != "99" || item.PublishedAt != 1700000010 || !strings.Contains(item.Description, "我的转发语") || !strings.Contains(item.Description, "转发内容") {
		t.Fatalf("forward metadata/original content missing: %#v", item)
	}

	nullOriginal := json.RawMessage(`{"type":"DYNAMIC_TYPE_FORWARD","id_str":"forward-null","modules":{"module_author":{"mid":12,"name":"转发作者","pub_ts":1700000020},"module_dynamic":{"desc":{"text":"只有转发语"}}},"orig":null}`)
	item, ok = dynamicItem(nullOriginal, 0)
	if !ok || item.Kind != "article" || item.ID != "bilibili:dynamic:forward-null" || item.Title != "只有转发语" {
		t.Fatalf("null orig should remain a readable article: ok=%t item=%#v", ok, item)
	}
	if item.Description != "只有转发语" || item.PreviewText != "只有转发语" || len(item.ArticleBlocks) != 1 || item.ArticleBlocks[0].Text != "只有转发语" {
		t.Fatalf("null orig body not preserved: %#v", item)
	}
}

func TestDynamicItemMapsDrawWordAndArticleContent(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		kind  string
		id    string
		url   string
		title string
		body  string
	}{
		{
			name: "draw",
			raw:  `{"type":"DYNAMIC_TYPE_DRAW","id_str":"draw-1","modules":{"module_author":{"mid":21,"name":"图文作者","pub_ts":1700000100},"module_dynamic":{"desc":{"text":"图文说明"},"major":{"draw":{"id":"draw-rid","items":[{"src":"//i0.hdslb.com/p1.jpg","width":"800","height":600},{"src":"https://i0.hdslb.com/p2.jpg","width":640,"height":"480"}]}}}}}`,
			kind: "image", id: "bilibili:dynamic:draw-1", url: "https://t.bilibili.com/draw-1", title: "图文说明", body: "图文说明",
		},
		{
			name: "word",
			raw:  `{"type":"DYNAMIC_TYPE_WORD","id_str":"word-1","modules":{"module_author":{"mid":"22","name":"文字作者","pub_ts":"1700000200"},"module_dynamic":{"desc":{"text":"纯文字动态"}}}}`,
			kind: "article", id: "bilibili:dynamic:word-1", url: "https://t.bilibili.com/word-1", title: "纯文字动态", body: "纯文字动态",
		},
		{
			name: "article",
			raw:  `{"type":"DYNAMIC_TYPE_ARTICLE","id_str":"article-1","modules":{"module_author":{"mid":23,"name":"专栏作者","pub_ts":1700000300},"module_dynamic":{"desc":{"text":"专栏摘要"},"major":{"article":{"id":123,"title":"专栏标题","jump_url":"//www.bilibili.com/read/cv123/","covers":["//i0.hdslb.com/article.jpg"]}}}}}`,
			kind: "article", id: "bilibili:dynamic:article-1", url: "https://www.bilibili.com/read/cv123/", title: "专栏标题", body: "专栏摘要",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			item, ok := dynamicItem(json.RawMessage(test.raw), 0)
			if !ok {
				t.Fatalf("dynamicItem rejected %s fixture: %#v", test.name, item)
			}
			if item.Kind != test.kind || item.ID != test.id || item.URL != test.url || item.Title != test.title || item.Description != test.body || item.PreviewText != test.body {
				t.Fatalf("unexpected %s mapping: %#v", test.name, item)
			}
			if len(item.ArticleBlocks) == 0 || item.ArticleBlocks[0].Text != test.body {
				t.Fatalf("%s body was not projected into reader blocks: %#v", test.name, item.ArticleBlocks)
			}
			if test.name == "draw" {
				if len(item.Images) != 2 || item.ThumbnailURL != "https://i0.hdslb.com/p1.jpg" || item.Images[0].Width != 800 || item.Images[0].Height != 600 {
					t.Fatalf("draw images were not normalized: %#v", item)
				}
			} else if test.name == "article" && item.ThumbnailURL != "https://i0.hdslb.com/article.jpg" {
				t.Fatalf("article cover missing: %#v", item)
			}
		})
	}
}

func TestDynamicVideoCompatibilityWrapperOnlyAcceptsVideos(t *testing.T) {
	word := json.RawMessage(`{"type":"DYNAMIC_TYPE_WORD","id_str":"word-wrapper","modules":{"module_dynamic":{"desc":{"text":"文字"}}}}`)
	if _, ok := dynamicVideo(word, 0); ok {
		t.Fatal("dynamicVideo compatibility wrapper accepted a non-video item")
	}
}

func TestDynamicItemMapsOtherMajorTypesToReadableCards(t *testing.T) {
	fixtures := []struct {
		name  string
		raw   string
		title string
		url   string
		cover string
	}{
		{"pgc", `{"type":"DYNAMIC_TYPE_PGC","id_str":"pgc-1","modules":{"module_author":{"mid":1,"name":"番剧作者","pub_ts":1700000400},"module_dynamic":{"major":{"pgc":{"title":"番剧标题","cover":"//i0.hdslb.com/pgc.jpg","jump_url":"//www.bilibili.com/bangumi/play/ep1"}}}}}`, "番剧标题", "https://www.bilibili.com/bangumi/play/ep1", "https://i0.hdslb.com/pgc.jpg"},
		{"courses", `{"type":"DYNAMIC_TYPE_COURSES","id_str":"course-1","modules":{"module_author":{"mid":2,"name":"课程作者","pub_ts":"1700000500"},"module_dynamic":{"major":{"courses":{"title":"课程标题","sub_title":"课程副标题","cover":"//i0.hdslb.com/course.jpg","jump_url":"//www.bilibili.com/cheese/play/1"}}}}}`, "课程标题", "https://www.bilibili.com/cheese/play/1", "https://i0.hdslb.com/course.jpg"},
		{"live", `{"type":"DYNAMIC_TYPE_LIVE_RCMD","id_str":"live-1","modules":{"module_author":{"mid":3,"name":"直播作者","pub_ts":1700000600},"module_dynamic":{"major":{"live_rcmd":{"content":"{\"live_play_info\":{\"title\":\"直播标题\",\"cover\":\"//i0.hdslb.com/live.jpg\",\"jump_url\":\"//live.bilibili.com/123\"}}"}}}}}`, "直播标题", "https://live.bilibili.com/123", "https://i0.hdslb.com/live.jpg"},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			item, ok := dynamicItem(json.RawMessage(fixture.raw), 0)
			if !ok || item.Title != fixture.title || item.URL != fixture.url || item.ThumbnailURL != fixture.cover || item.Author == "" || item.PublishedAt == 0 {
				t.Fatalf("other dynamic type was not mapped: ok=%t item=%#v", ok, item)
			}
		})
	}
}
