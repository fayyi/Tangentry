package notes

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// addMarkdownExportFiles writes the index and source-oriented Markdown views.
// ZIP entry creation stays in exportArchive so the presentation can evolve
// independently from export selection and note validation.
func addMarkdownExportFiles(ctx context.Context, archive *exportArchive, entries []ExportEntry, index string, at time.Time) error {
	files := []struct {
		name string
		data []byte
	}{
		{"INDEX.md", []byte(index)},
		{"SOURCES.md", []byte(sourceIndex(entries))},
		{"TAGS.md", []byte(tagIndex(entries))},
		{"README.md", []byte(exportReadme)},
	}
	for _, file := range files {
		if err := archive.Add(ctx, file.name, file.data, at); err != nil {
			return err
		}
	}
	for _, document := range sourceDocuments(entries) {
		if err := archive.Add(ctx, document.name, []byte(document.body), at); err != nil {
			return err
		}
	}
	return nil
}

func tagIndex(entries []ExportEntry) string {
	groups := make(map[string][]ExportEntry)
	for _, entry := range entries {
		seen := make(map[string]bool)
		for _, tag := range entry.Note.Tags {
			if !seen[tag] {
				groups[tag] = append(groups[tag], entry)
				seen[tag] = true
			}
		}
	}
	tags := make([]string, 0, len(groups))
	for tag := range groups {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	var index strings.Builder
	index.WriteString("# 按标签整理的笔记\n\n[返回时间索引](INDEX.md) · [按来源查看](SOURCES.md)\n\n")
	if len(tags) == 0 {
		index.WriteString("这次导出的笔记没有标签。\n")
	}
	for _, tag := range tags {
		fmt.Fprintf(&index, "## #%s\n\n%d 条笔记\n\n", markdownLabel(tag), len(groups[tag]))
		for _, entry := range groups[tag] {
			fmt.Fprintf(&index, "- %s [%s](notes/%s)\n", time.Unix(entry.Note.CreatedAt, 0).UTC().Format("2006-01-02 15:04:05 UTC"), markdownLabel(noteTitle(entry.Note.Body)), url.PathEscape(strings.TrimPrefix(entry.File, "notes/")))
		}
		index.WriteString("\n")
	}
	return index.String()
}

func sourceIndex(entries []ExportEntry) string {
	groups := make(map[string][]ExportEntry)
	order := []string{}
	for _, entry := range entries {
		id := entry.Note.ContentID
		if strings.HasPrefix(id, "memo:") {
			id = ""
		}
		if _, exists := groups[id]; !exists {
			order = append(order, id)
		}
		groups[id] = append(groups[id], entry)
	}
	var index strings.Builder
	index.WriteString("# 按来源整理的笔记\n\n[返回时间索引](INDEX.md)\n\n")
	for _, id := range order {
		group := groups[id]
		title := "随手记"
		var source *ExportSource
		for _, entry := range group {
			if entry.Source != nil && entry.Source.Title != "" {
				source = entry.Source
				title = strings.Join(strings.Fields(source.Title), " ")
				break
			}
		}
		fmt.Fprintf(&index, "## %s\n\n", markdownLabel(title))
		if id != "" {
			fmt.Fprintf(&index, "内容标识：`%s` · %d 条笔记\n\n", id, len(group))
		} else {
			fmt.Fprintf(&index, "%d 条随手记\n\n", len(group))
		}
		if source != nil {
			writeIndexContext(&index, ExportEntry{Source: source})
			index.WriteString("\n")
		}
		for _, entry := range group {
			fmt.Fprintf(&index, "- %s [%s](notes/%s)\n", time.Unix(entry.Note.CreatedAt, 0).UTC().Format("2006-01-02 15:04:05 UTC"), markdownLabel(noteTitle(entry.Note.Body)), url.PathEscape(strings.TrimPrefix(entry.File, "notes/")))
			entry.Source = nil
			writeIndexContext(&index, entry)
		}
		index.WriteString("\n")
	}
	return index.String()
}

func writeIndexContext(index *strings.Builder, entry ExportEntry) {
	if source := entry.Source; source != nil && source.Title != "" {
		label := markdownLabel(strings.Join(strings.Fields(source.Title), " "))
		if source.URL != "" {
			destination := strings.NewReplacer("<", "%3C", ">", "%3E", "\n", "%0A", "\r", "%0D").Replace(source.URL)
			fmt.Fprintf(index, "  - 来源：[%s](<%s>)\n", label, destination)
		} else {
			fmt.Fprintf(index, "  - 来源：%s\n", label)
		}
	}
	if position := entry.Note.PositionSeconds; position != nil {
		seconds := int64(*position)
		if link := originalTimeLink(entry.Note.ContentID, seconds); link != "" {
			fmt.Fprintf(index, "  - 关联时间：[%s](%s)\n", positionLabel(seconds), link)
		} else {
			fmt.Fprintf(index, "  - 关联时间：%s\n", positionLabel(seconds))
		}
	}
	quote := ""
	if anchor := entry.Note.TextAnchor; anchor != nil {
		quote = anchor.Quote
	}
	if anchor := entry.Note.PDFAnchor; anchor != nil {
		quote = anchor.Quote
		fmt.Fprintf(index, "  - PDF：第 %d 页\n", anchor.Page)
	}
	if quote != "" {
		index.WriteString("\n")
		for _, line := range strings.Split(strings.ReplaceAll(quote, "\r\n", "\n"), "\n") {
			literal := markdownLabel(strings.TrimSuffix(line, "\r"))
			literal = strings.NewReplacer("#", "\\#", "-", "\\-", "+", "\\+", ".", "\\.", "~", "\\~", "|", "\\|").Replace(literal)
			fmt.Fprintf(index, "  > %s\n", literal)
		}
		index.WriteString("\n")
	}
}
