package httptransport

import (
	"net/http"
	"strings"
	"tangentry/internal/documents"
	"time"
)

func (h *libraryHandler) documentUpload(w http.ResponseWriter, r *http.Request) {
	if h.library.Documents == nil {
		fail(w, documents.ErrUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, documents.MaxSize+(1<<20))
	if r.ParseMultipartForm(1<<20) != nil {
		fail(w, documents.ErrInvalid)
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		fail(w, documents.ErrInvalid)
		return
	}
	defer file.Close()
	item, err := h.library.Documents.Import(r.Context(), file, header.Filename, "http://"+r.Host+"/library")
	if err != nil {
		fail(w, err)
		return
	}
	// Importing a file is an explicit library action.
	item.URL = "http://" + r.Host + "/api/documents/" + item.PlatformID + "/file"
	saved, err := h.library.Content.Save(r.Context(), item)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 201, saved)
}
func (h *libraryHandler) documentFile(w http.ResponseWriter, r *http.Request) {
	if h.library.Documents == nil {
		fail(w, documents.ErrUnavailable)
		return
	}
	file, err := h.library.Documents.File(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		fail(w, documents.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", "inline; filename=\"document.pdf\"")
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	http.ServeContent(w, r, "document.pdf", info.ModTime(), file)
}
func (h *libraryHandler) documentGet(w http.ResponseWriter, r *http.Request) {
	if h.library.Documents == nil {
		fail(w, documents.ErrUnavailable)
		return
	}
	item, err := h.library.Documents.Get(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	if strings.HasSuffix(item.URL, "/library") {
		item.URL = "http://" + r.Host + "/api/documents/" + item.PlatformID + "/file"
	}
	write(w, 200, item)
}
