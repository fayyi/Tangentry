package httptransport

import (
	"context"

	"tangentry/internal/history"
	"tangentry/internal/notes"
	"tangentry/internal/readingprogress"
	"tangentry/internal/readstate"
)

type NoteService interface {
	SearchFiltered(context.Context, string, string, notes.Filters) (notes.SearchPage, error)
	Tags(context.Context) ([]notes.TagCount, error)
	List(context.Context, string) ([]notes.Note, error)
	Create(context.Context, notes.Input) (notes.Note, error)
	Edit(context.Context, string, notes.Edit) (notes.Note, error)
	Delete(context.Context, string, ...int64) error
	SetPinned(context.Context, string, bool) (notes.Note, error)
}

type NoteFrameService interface {
	Capture(context.Context, string, notes.FrameInput) (notes.Note, error)
	Create(context.Context, notes.FrameNoteInput) (notes.Note, error)
	CreateWithImage(context.Context, notes.ImageNoteInput) (notes.Note, error)
	AttachImage(context.Context, string, int64, notes.FrameImage) (notes.Note, error)
	AppendImage(context.Context, string, int64, notes.FrameImage) (notes.Note, error)
	RemoveOne(context.Context, string, int64, string) (notes.Note, error)
	Remove(context.Context, string, int64) (notes.Note, error)
	Image(context.Context, string, string) ([]byte, error)
}

type NoteExportService interface {
	Export(context.Context, notes.ExportInput) (notes.ExportResult, error)
}

type HistoryService interface {
	Viewing(context.Context) ([]history.Entry, error)
	Record(context.Context, []history.Entry) (int, error)
	RemoveViewing(context.Context, string) error
	ClearViewing(context.Context) error
	Searches(context.Context) ([]history.Search, error)
	AddSearches(context.Context, []history.Search) (int, error)
	RemoveSearch(context.Context, string) error
	ClearSearches(context.Context) error
}

type ProgressService interface {
	Get(context.Context, string) (*readingprogress.Progress, error)
	Batch(context.Context, []string) ([]readingprogress.Progress, error)
	Save(context.Context, string, readingprogress.Input) (readingprogress.Progress, error)
}

type ReadStateService interface {
	Batch(context.Context, []string) ([]readstate.State, error)
	Set(context.Context, []string, bool) ([]readstate.State, error)
}
