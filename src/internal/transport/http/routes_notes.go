package httptransport

import "net/http"

func (h *Handler) registerNotesRoutes(mux *http.ServeMux) {
	// History is persisted by the notes-side application services and shares
	// the same reader context as notes, read state, and progress.
	mux.HandleFunc("GET /api/history/viewing", h.notes.viewingList)
	mux.HandleFunc("POST /api/history/viewing", h.notes.viewingRecord)
	mux.HandleFunc("DELETE /api/history/viewing", h.notes.viewingClear)
	mux.HandleFunc("DELETE /api/history/viewing/{id}", h.notes.viewingRemove)
	mux.HandleFunc("GET /api/history/searches", h.notes.searchList)
	mux.HandleFunc("POST /api/history/searches", h.notes.searchAdd)
	mux.HandleFunc("DELETE /api/history/searches", h.notes.searchClear)
	mux.HandleFunc("DELETE /api/history/searches/{id}", h.notes.searchRemove)

	mux.HandleFunc("GET /api/notes", h.notes.noteList)
	mux.HandleFunc("GET /api/notes/search", h.notes.noteSearch)
	mux.HandleFunc("POST /api/notes/export", h.notes.noteExport)
	mux.HandleFunc("GET /api/notes/tags", h.notes.noteTags)
	mux.HandleFunc("POST /api/notes", h.notes.noteCreate)
	mux.HandleFunc("POST /api/notes/with-frame", h.notes.noteWithFrame)
	mux.HandleFunc("POST /api/notes/with-image", h.notes.noteWithImage)
	mux.HandleFunc("PATCH /api/notes/{id}", h.notes.noteEdit)
	mux.HandleFunc("PUT /api/notes/{id}/pin", h.notes.notePin)
	mux.HandleFunc("DELETE /api/notes/{id}", h.notes.noteDelete)
	mux.HandleFunc("POST /api/notes/{id}/frame", h.notes.noteFrameCapture)
	mux.HandleFunc("POST /api/notes/{id}/frames", h.notes.noteFrameAppend)
	mux.HandleFunc("POST /api/notes/{id}/frames/image", h.notes.noteFrameImageAppend)
	mux.HandleFunc("DELETE /api/notes/{id}/frames/{frameID}", h.notes.noteFrameOneRemove)
	mux.HandleFunc("DELETE /api/notes/{id}/frame", h.notes.noteFrameRemove)
	mux.HandleFunc("PUT /api/notes/{id}/frame-image", h.notes.noteFrameImageAttach)
	mux.HandleFunc("GET /api/notes/{id}/frames/{frameID}", h.notes.noteFrameImage)
	mux.HandleFunc("GET /api/read-state", h.notes.readStateBatch)
	mux.HandleFunc("PUT /api/read-state", h.notes.readStateSet)
	mux.HandleFunc("GET /api/reading-progress", h.notes.progressGet)
	mux.HandleFunc("GET /api/reading-progress/batch", h.notes.progressBatch)
	mux.HandleFunc("PUT /api/reading-progress/{id}", h.notes.progressSave)
}
