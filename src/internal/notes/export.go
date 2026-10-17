package notes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"
)

const ExportMaxNotes = 10_000
const ExportMaxSnapshotBytes = 16 << 20
const ExportMaxArchiveBytes = 32 << 20 // uncompressed archive payload

var ErrExportBusy = errors.New("note export busy")
var ErrExportLimit = errors.New("note export limit")
var ErrExportEmpty = errors.New("no saved notes to export")
var ErrExportUnavailable = errors.New("note export unavailable")

type ExportInput struct {
	PinnedOnly bool   `json:"pinnedOnly"`
	Query      string `json:"q"`
	Tag        string `json:"tag"`
	From       int64  `json:"from"`
	To         int64  `json:"to"`
	ContentID  string `json:"contentId"`
}

// This is the complete whitelist of local source metadata that leaves the repository.
type ExportSource struct {
	Title    string `json:"title"`
	URL      string `json:"url"`
	Author   string `json:"author,omitempty"`
	Platform string `json:"platform,omitempty"`
	Kind     string `json:"kind,omitempty"`
}
type ExportEntry struct {
	Note            Note            `json:"note"`
	Source          *ExportSource   `json:"source,omitempty"`
	File            string          `json:"file"`
	FrameData       []byte          `json:"-"`
	FramesData      [][]byte        `json:"-"`
	FrameFile       string          `json:"frameFile,omitempty"`
	FrameMissing    bool            `json:"frameMissing,omitempty"`
	FrameMissingIDs map[string]bool `json:"-"`
}
type ExportRepository interface {
	ExportNotes(context.Context, SearchQuery, string) ([]ExportEntry, error)
}
type ExportResult struct {
	Data     []byte
	Filename string
	Count    int
}
type ExportService struct {
	repository ExportRepository
	active     chan struct{}
}

func NewExporter(repository ExportRepository) *ExportService {
	return &ExportService{repository: repository, active: make(chan struct{}, 1)}
}

func (s *ExportService) Export(ctx context.Context, input ExportInput) (ExportResult, error) {
	query, err := normalizeSearch(input.Query, Filters{Tag: input.Tag, From: input.From, To: input.To, PinnedOnly: input.PinnedOnly})
	if err != nil || input.ContentID != "" && !validContentID(input.ContentID) {
		return ExportResult{}, ErrInvalid
	}
	select {
	case s.active <- struct{}{}:
		defer func() { <-s.active }()
	default:
		return ExportResult{}, ErrExportBusy
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	entries, err := s.repository.ExportNotes(ctx, query, input.ContentID)
	if err != nil {
		return ExportResult{}, err
	}
	if len(entries) == 0 {
		return ExportResult{}, ErrExportEmpty
	}
	if len(entries) > ExportMaxNotes {
		return ExportResult{}, ErrExportLimit
	}
	input.Query = query.Text
	input.Tag = query.Tag
	return archiveNotes(ctx, entries, input, time.Now().UTC())
}

func archiveNotes(ctx context.Context, entries []ExportEntry, selection ExportInput, at time.Time) (ExportResult, error) {
	archive := newExportArchive()
	var index strings.Builder
	index.WriteString("# Tangentry 笔记\n\n")
	fmt.Fprintf(&index, "共 %d 条已保存笔记。时间使用 UTC；筛选条件和完整关联数据见 tangentry-notes.json。\n\n", len(entries))
	index.WriteString("[按来源查看笔记](SOURCES.md) · [按标签查看笔记](TAGS.md)\n\n")
	seen := map[string]bool{}
	for i := range entries {
		entry := &entries[i]
		note := entry.Note
		if !noteIDPattern.MatchString(note.ID) || !validContentID(note.ContentID) || seen[note.ID] || validate(note.Body, note.PositionSeconds) != nil || validateAnchor(note.TextAnchor) != nil || validatePDFAnchor(note.PDFAnchor) != nil || note.TextAnchor != nil && note.PDFAnchor != nil || note.CreatedAt < 0 || note.UpdatedAt < 0 || note.CreatedAt > 253402300799 || note.UpdatedAt > 253402300799 {
			return ExportResult{}, ErrExportUnavailable
		}
		seen[note.ID] = true
		if ValidateSubtitleReferences(note.SubtitleReferences) != nil || len(note.SubtitleReferences) > 0 && (note.TextAnchor != nil || note.PDFAnchor != nil || strings.HasPrefix(note.ContentID, "memo:")) {
			return ExportResult{}, ErrExportUnavailable
		}
		if note.Tags == nil {
			note.Tags = []string{}
			entry.Note.Tags = note.Tags
		}
		if _, err := normalizeTags(note.Tags); err != nil {
			return ExportResult{}, ErrExportUnavailable
		}
		entry.Source = exportSource(note.ContentID, entry.Source)
		title := noteTitle(note.Body)
		created := time.Unix(note.CreatedAt, 0).UTC()
		entry.File = "notes/" + created.Format("2006-01-02") + "-" + filenameTitle(title) + "--" + note.ID + ".md"
		properties := map[string]any{"tangentry_note_id": note.ID, "tangentry_content_id": note.ContentID, "created": created.Format(time.RFC3339), "updated": time.Unix(note.UpdatedAt, 0).UTC().Format(time.RFC3339), "tags": note.Tags}
		properties["pinned"] = note.Pinned
		if entry.Source != nil {
			if entry.Source.Title != "" {
				properties["source_title"] = entry.Source.Title
			}
			if entry.Source.URL != "" {
				properties["source_url"] = entry.Source.URL
			}
			if entry.Source.Author != "" {
				properties["source_author"] = entry.Source.Author
			}
		}
		if note.PositionSeconds != nil {
			seconds := int64(math.Floor(*note.PositionSeconds))
			properties["position_seconds"] = seconds
			properties["position"] = positionLabel(seconds)
			if link := originalTimeLink(note.ContentID, seconds); link != "" {
				properties["source_url_at_time"] = link
			}
		}
		if anchor := note.TextAnchor; anchor != nil {
			properties["quoted_text"] = anchor.Quote
			properties["source_hash"] = anchor.SourceHash
			properties["quote_start_utf16"] = anchor.Start
			properties["quote_end_utf16"] = anchor.End
		}
		if anchor := note.PDFAnchor; anchor != nil {
			properties["quoted_text"] = anchor.Quote
			properties["pdf_page"] = anchor.Page
			properties["pdf_document_id"] = anchor.DocumentID
		}
		if len(note.SubtitleReferences) > 0 {
			properties["subtitle_references"] = note.SubtitleReferences
		}
		frames := note.Frames
		if len(frames) == 0 && note.Frame != nil {
			frames = []Frame{*note.Frame}
		}
		frameData := entry.FramesData
		if len(frameData) == 0 && len(frames) == 1 && len(entry.FrameData) > 0 {
			frameData = [][]byte{entry.FrameData}
		}
		if len(frames) > 0 {
			properties["video_frame"] = frames[0]
			properties["video_frames"] = frames
		}
		for i := range frames {
			frame := &frames[i]
			missing := entry.FrameMissingIDs[frame.ID] || i >= len(frameData)
			if ValidateFrame(frame) != nil || !missing && (len(frameData[i]) == 0 || len(frameData[i]) > MaxFrameBytes) {
				return ExportResult{}, ErrExportUnavailable
			}
			if missing {
				continue
			}
			name := "images/" + frame.ID + ".jpg"
			if i == 0 {
				entry.FrameFile = name
			}
			if err := archive.Add(ctx, name, frameData[i], time.Unix(frame.CreatedAt, 0)); err != nil {
				return ExportResult{}, err
			}
			if i == 0 {
				properties["video_frame_file"] = "../" + name
			}
		}
		metadata, err := json.MarshalIndent(properties, "", "  ")
		if err != nil {
			return ExportResult{}, err
		}
		// JSON frontmatter is supported by Obsidian. The saved Markdown body is
		// reproduced unchanged under its own heading after its context.
		body := append([]byte("---\n"), metadata...)
		body = append(body, []byte("\n---\n\n"+noteDocument(*entry, "../", 2))...)
		if err = archive.Add(ctx, entry.File, body, time.Unix(note.UpdatedAt, 0)); err != nil {
			return ExportResult{}, err
		}
		fmt.Fprintf(&index, "- %s [%s](%s)\n", created.Format("2006-01-02 15:04:05 UTC"), markdownLabel(title), "notes/"+url.PathEscape(strings.TrimPrefix(entry.File, "notes/")))
		writeIndexContext(&index, *entry)
	}
	manifest := struct {
		SchemaVersion int           `json:"schemaVersion"`
		ExportedAt    string        `json:"exportedAt"`
		Selection     ExportInput   `json:"selection"`
		Entries       []ExportEntry `json:"entries"`
	}{1, at.Format(time.RFC3339), selection, entries}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return ExportResult{}, err
	}
	if err = archive.Add(ctx, "tangentry-notes.json", data, at); err != nil {
		return ExportResult{}, err
	}
	if err = addMarkdownExportFiles(ctx, archive, entries, index.String(), at); err != nil {
		return ExportResult{}, err
	}
	data, err = archive.Close(ctx)
	if err != nil {
		return ExportResult{}, err
	}
	return ExportResult{Data: data, Filename: "Tangentry-notes-" + at.Format("20060102-150405") + ".zip", Count: len(entries)}, nil
}
