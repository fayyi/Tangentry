package httptransport

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/notes"
)

func TestNoteExportHTTPDownloadAllMatchingSavedNotesAndBoundaries(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "export.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := notes.NewService(store)
	for i := 0; i < 65; i++ {
		if _, err := service.Create(context.Background(), notes.Input{Body: fmt.Sprintf("导出笔记 %d", i), Tags: []string{"学习"}}); err != nil {
			t.Fatal(err)
		}
	}
	position := 5.333631
	if _, err := service.Create(context.Background(), notes.Input{ContentID: "youtube:abcdefghijk", Body: "source note", PositionSeconds: &position}); err != nil {
		t.Fatal(err)
	}
	h := New(nil, nil, nil, Options{Notes: service, NoteExports: notes.NewExporter(store)})
	send := func(method, target, body, contentType, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1"+target, strings.NewReader(body))
		r.Header.Set("Content-Type", contentType)
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, selection := range []struct {
		body  string
		count int
	}{{`{"q":"导出笔记","tag":"学习"}`, 65}, {`{"contentId":"youtube:abcdefghijk"}`, 1}} {
		w := send("POST", "/api/notes/export", selection.body, "application/json; charset=utf-8", "http://127.0.0.1")
		if w.Code != 200 || w.Header().Get("Content-Type") != "application/zip" || w.Header().Get("X-Tangentry-Notes-Count") != strconv.Itoa(selection.count) || w.Header().Get("Content-Length") != strconv.Itoa(w.Body.Len()) || w.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment;") {
			t.Fatal(w.Code, w.Header(), w.Body.String())
		}
		archive, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
		if err != nil {
			t.Fatal(err)
		}
		notesFiles, sourceFiles := 0, 0
		for _, file := range archive.File {
			if strings.HasPrefix(file.Name, "notes/") {
				notesFiles++
			}
			if strings.HasPrefix(file.Name, "sources/") {
				sourceFiles++
			}
		}
		if notesFiles != selection.count || len(archive.File) != selection.count+5+sourceFiles {
			t.Fatal("incomplete download", len(archive.File))
		}
		for _, file := range archive.File {
			stream, err := file.Open()
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(stream)
			stream.Close()
			if err != nil {
				t.Fatal(err)
			}
			if file.Name == "tangentry-notes.json" {
				var manifest struct {
					Entries []notes.ExportEntry `json:"entries"`
				}
				if json.Unmarshal(data, &manifest) != nil || len(manifest.Entries) != selection.count {
					t.Fatal("invalid manifest")
				}
			}
		}
	}
	for _, input := range []struct {
		method, target, body, mime, origin string
		status                             int
	}{
		{"GET", "/api/notes/export", "", "", "", 404},
		{"POST", "/api/notes/export", `{}`, "text/plain", "", 415},
		{"POST", "/api/notes/export", `{"unknown":"secret"}`, "application/json", "", 400},
		{"POST", "/api/notes/export", `{} {}`, "application/json", "", 400},
		{"POST", "/api/notes/export?q=other", `{}`, "application/json", "", 400},
		{"POST", "/api/notes/export", strings.Repeat(" ", 4096) + `{}`, "application/json", "", 400},
		{"POST", "/api/notes/export", `{"from":5,"to":5}`, "application/json", "", 400},
		{"POST", "/api/notes/export", `{"contentId":"../../escape"}`, "application/json", "", 400},
		{"POST", "/api/notes/export", `{"q":"no saved matching notes"}`, "application/json", "", 404},
		{"POST", "/api/notes/export", `{}`, "application/json", "https://evil.example", 403},
	} {
		w := send(input.method, input.target, input.body, input.mime, input.origin)
		if w.Code != input.status || w.Header().Get("Content-Disposition") != "" || w.Header().Get("X-Tangentry-Notes-Count") != "" || strings.HasPrefix(w.Body.String(), "PK") || strings.Contains(w.Body.String(), "secret") {
			t.Fatal(input, w.Code, w.Header(), w.Body.String())
		}
	}
	r := httptest.NewRequest("POST", "http://evil.example/api/notes/export", strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("untrusted host accepted")
	}
}

type failedNoteExportRepository struct{ err error }

func (f failedNoteExportRepository) ExportNotes(context.Context, notes.SearchQuery, string) ([]notes.ExportEntry, error) {
	return nil, f.err
}
func TestNoteExportHTTPFailureNeverWritesPartialArchive(t *testing.T) {
	for _, value := range []struct {
		err    error
		status int
	}{{notes.ErrExportLimit, 413}, {notes.ErrExportUnavailable, 500}} {
		h := New(nil, nil, nil, Options{NoteExports: notes.NewExporter(failedNoteExportRepository{value.err})})
		r := httptest.NewRequest("POST", "http://127.0.0.1/api/notes/export", strings.NewReader(`{}`))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != value.status || w.Header().Get("Content-Disposition") != "" || w.Header().Get("X-Tangentry-Notes-Count") != "" || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
			t.Fatal(w.Code, w.Header(), w.Body.String())
		}
	}
	// A disconnected client receives no body or attachment headers.
	h := New(nil, nil, nil, Options{NoteExports: notes.NewExporter(failedNoteExportRepository{context.Canceled})})
	r := httptest.NewRequest("POST", "http://127.0.0.1/api/notes/export", strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Body.Len() != 0 || w.Header().Get("Content-Disposition") != "" || w.Header().Get("X-Tangentry-Notes-Count") != "" {
		t.Fatal("cancellation wrote an archive", w.Header(), w.Body.String())
	}
}
