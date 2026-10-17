package notes

import (
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
)

func noteDocument(entry ExportEntry, root string, level int) string {
	note := entry.Note
	heading := strings.Repeat("#", level) + " "
	var out strings.Builder
	timed := note.PositionSeconds != nil || len(note.SubtitleReferences) > 0
	if timed {
		out.WriteString(heading + "关联时间\n\n")
		if note.PositionSeconds != nil {
			seconds := int64(math.Floor(*note.PositionSeconds))
			if link := originalTimeLink(note.ContentID, seconds); link != "" {
				fmt.Fprintf(&out, "[%s](%s)\n\n", positionLabel(seconds), link)
			} else {
				fmt.Fprintf(&out, "%s\n\n", positionLabel(seconds))
			}
		}
		for _, reference := range note.SubtitleReferences {
			label := positionLabel(int64(math.Floor(reference.Start))) + "–" + positionLabel(int64(math.Floor(reference.End)))
			if link := originalTimeLink(note.ContentID, int64(math.Floor(reference.Start))); link != "" {
				fmt.Fprintf(&out, "[%s](%s)", label, link)
			} else {
				out.WriteString(label)
			}
			fmt.Fprintf(&out, " · %s\n\n", markdownLabel(reference.Language))
			for _, line := range strings.Split(reference.Text, "\n") {
				fmt.Fprintf(&out, "> %s\n", markdownLabel(line))
			}
			out.WriteString("\n")
		}
	}
	frames := note.Frames
	if len(frames) == 0 && note.Frame != nil {
		frames = []Frame{*note.Frame}
	}
	if len(frames) > 0 {
		out.WriteString(heading + "视频画面\n\n")
		for _, frame := range frames {
			label := positionLabel(int64(math.Floor(frame.PositionSeconds)))
			if frame.Source != FrameSourcePlayer {
				label = "约 " + label
			}
			file := "images/" + frame.ID + ".jpg"
			if entry.FrameMissingIDs[frame.ID] || file == "" {
				fmt.Fprintf(&out, "%s；图片未能读取，时间和来源记录仍保留。\n\n", label)
			} else {
				fmt.Fprintf(&out, "![视频画面 %s](%s%s)\n\n", label, root, file)
			}
		}
	}
	if timed || len(frames) > 0 {
		out.WriteString(heading + "我的记录\n\n")
	}
	out.WriteString(note.Body + "\n")
	return out.String()
}

type exportDocument struct{ name, body string }

func sourceDocuments(entries []ExportEntry) []exportDocument {
	groups := make(map[string][]ExportEntry)
	order := []string{}
	for _, entry := range entries {
		id := entry.Note.ContentID
		if strings.HasPrefix(id, "memo:") {
			continue
		}
		if _, exists := groups[id]; !exists {
			order = append(order, id)
		}
		groups[id] = append(groups[id], entry)
	}
	used := map[string]bool{}
	documents := make([]exportDocument, 0, len(order))
	for _, id := range order {
		group := groups[id]
		sort.SliceStable(group, func(i, j int) bool {
			left, right := entryPosition(group[i]), entryPosition(group[j])
			if (left < 0) != (right < 0) {
				return left >= 0
			}
			if left != right {
				return left < right
			}
			return group[i].Note.CreatedAt < group[j].Note.CreatedAt
		})
		var source *ExportSource
		for _, entry := range group {
			if entry.Source != nil && entry.Source.Title != "" {
				source = entry.Source
				break
			}
		}
		title := id
		if source != nil {
			title = strings.Join(strings.Fields(source.Title), " ")
		}
		name := "sources/" + filenameTitle(title) + ".md"
		for suffix := 2; used[name]; suffix++ {
			name = fmt.Sprintf("sources/%s-%d.md", filenameTitle(title), suffix)
		}
		used[name] = true
		var out strings.Builder
		fmt.Fprintf(&out, "# %s\n\n", markdownLabel(title))
		if source != nil && source.URL != "" {
			destination := strings.NewReplacer("<", "%3C", ">", "%3E", "\n", "%0A", "\r", "%0D").Replace(source.URL)
			fmt.Fprintf(&out, "来源：[%s](<%s>)", markdownLabel(title), destination)
			if source.Author != "" {
				fmt.Fprintf(&out, " · %s", markdownLabel(source.Author))
			}
			out.WriteString("  \n")
		}
		fmt.Fprintf(&out, "%d 条笔记 · [返回来源索引](../SOURCES.md)\n", len(group))
		for _, entry := range group {
			out.WriteString("\n---\n\n")
			label := noteTitle(entry.Note.Body)
			if position := entryPosition(entry); position >= 0 {
				label = positionLabel(int64(math.Floor(position))) + " · " + label
			}
			fmt.Fprintf(&out, "## %s\n\n", markdownLabel(label))
			out.WriteString(noteDocument(entry, "../", 3))
			if len(entry.Note.Tags) > 0 {
				out.WriteString("\n")
				for i, tag := range entry.Note.Tags {
					if i > 0 {
						out.WriteString(" ")
					}
					out.WriteString("#" + strings.ReplaceAll(tag, " ", "-"))
				}
				out.WriteString("\n")
			}
			fmt.Fprintf(&out, "\n[单条笔记](../notes/%s)\n", url.PathEscape(strings.TrimPrefix(entry.File, "notes/")))
		}
		documents = append(documents, exportDocument{name, out.String()})
	}
	return documents
}

func entryPosition(entry ExportEntry) float64 {
	if entry.Note.PositionSeconds != nil {
		return *entry.Note.PositionSeconds
	}
	if len(entry.Note.SubtitleReferences) > 0 {
		return entry.Note.SubtitleReferences[0].Start
	}
	if len(entry.Note.Frames) > 0 {
		return entry.Note.Frames[0].PositionSeconds
	}
	if entry.Note.Frame != nil {
		return entry.Note.Frame.PositionSeconds
	}
	return -1
}
