package notes

import (
	"strings"
	"testing"
)

func TestIndexContextEscapesUntrustedTextAndDoesNotInventTimeLinks(t *testing.T) {
	position := 47.9
	var index strings.Builder
	writeIndexContext(&index, ExportEntry{
		Note:   Note{ContentID: "rss:" + strings.Repeat("a", 64), PositionSeconds: &position, TextAnchor: &TextAnchor{Quote: "<script>alert(1)</script>\r\n[x](https://wrong.example)\n![image](bad)"}},
		Source: &ExportSource{Title: "A [title]\ncontinued", URL: "https://example.com/story_(part)?q=<tag>"},
	})
	text := index.String()
	for _, expected := range []string{"来源：[A \\[title\\] continued](<https://example.com/story_(part)?q=%3Ctag%3E>)", "关联时间：0:47", "> &lt;script&gt;", "> \\[x\\](https://wrong\\.example)", "> !\\[image\\](bad)"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %q in %s", expected, text)
		}
	}
	if strings.Contains(text, "<script>") || strings.Contains(text, "关联时间：[") || strings.Contains(text, "\r") {
		t.Fatal("unsafe text or fabricated timed link", text)
	}
}

func TestSourceIndexGroupsByIdentityAndLinksEveryExportedNote(t *testing.T) {
	position := 47.9
	entries := []ExportEntry{
		{Note: Note{ContentID: "youtube:abcdefghijk", Body: "First thought", PositionSeconds: &position}, File: "notes/first.md", Source: &ExportSource{Title: "Same [title]\n<script>", URL: "https://example.com/a"}},
		{Note: Note{ContentID: "youtube:other000001", Body: "Other source"}, File: "notes/other.md", Source: &ExportSource{Title: "Same [title]\n<script>", URL: "https://example.com/b"}},
		{Note: Note{ContentID: "youtube:abcdefghijk", Body: "Second thought"}, File: "notes/second.md"},
		{Note: Note{ContentID: "memo:" + strings.Repeat("a", 32), Body: "Memo"}, File: "notes/memo.md"},
		{Note: Note{ContentID: "memo:" + strings.Repeat("b", 32), Body: "Other memo"}, File: "notes/other-memo.md"},
	}
	index := sourceIndex(entries)
	for _, text := range []string{"[返回时间索引](INDEX.md)", "内容标识：`youtube:abcdefghijk` · 2 条笔记", "## 随手记", "2 条随手记", "关联时间：[0:47]", "[First thought](notes/first.md)", "[Second thought](notes/second.md)", "[Other source](notes/other.md)", "[Memo](notes/memo.md)"} {
		if !strings.Contains(index, text) {
			t.Fatalf("missing %q: %s", text, index)
		}
	}
	if strings.Count(index, "## Same \\[title\\] &lt;script&gt;") != 2 || strings.Contains(index, "<script>") {
		t.Fatal("merged distinct identities or unescaped title", index)
	}
	if strings.Index(index, "Second thought") > strings.Index(index, "Other source") {
		t.Fatal("same-source notes remain scattered", index)
	}
}

func TestTagIndexPreservesHierarchyAndIncludesOnlySelectedEntries(t *testing.T) {
	entries := []ExportEntry{
		{Note: Note{Body: "First [thought]", Tags: []string{"读书/摘录", "回顾", "回顾"}}, File: "notes/first note.md"},
		{Note: Note{Body: "Second thought", Tags: []string{"读书/摘录"}}, File: "notes/second.md"},
		{Note: Note{Body: "Untagged"}, File: "notes/untagged.md"},
	}
	index := tagIndex(entries)
	for _, text := range []string{"## #读书/摘录", "2 条笔记", "[First \\[thought\\]](notes/first%20note.md)", "[Second thought](notes/second.md)"} {
		if !strings.Contains(index, text) {
			t.Fatalf("missing %q in %s", text, index)
		}
	}
	if strings.Count(index, "first%20note.md") != 2 || strings.Contains(index, "Untagged") || strings.Contains(index, "## #读书\n") {
		t.Fatal("tags duplicated or parent membership invented", index)
	}
	if !strings.Contains(tagIndex(entries[2:]), "没有标签") {
		t.Fatal("missing empty state")
	}
}
