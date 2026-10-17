package httptransport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/downloads"
	"testing"
	"time"
)

type inactiveMediaRunner struct{}

type delayedMediaWriter struct{ http.ResponseWriter }

func (w delayedMediaWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w delayedMediaWriter) Write(data []byte) (int, error) {
	time.Sleep(40 * time.Millisecond)
	return w.ResponseWriter.Write(data)
}

func (inactiveMediaRunner) Ready() bool { return false }
func (inactiveMediaRunner) Download(context.Context, string, string, int) (string, error) {
	return "", errors.New("media route must not start a downloader")
}

func TestMediaUsesCompletedFileBoundaryAndHTTPRanges(t *testing.T) {
	root := t.TempDir()
	store, err := sqlite.Open(filepath.Join(root, "media.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := downloads.NewService(store, inactiveMediaRunner{}, filepath.Join(root, "media"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	ctx := context.Background()
	job := downloads.Job{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", URL: "https://www.bilibili.com/video/BV17x411w7KC/", Title: "range fixture", State: "completed", FileName: "video.mp4", CreatedAt: 1}
	data := []byte("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ explicit byte-range test, not codec proof")
	directory := filepath.Join(root, "media", job.ID)
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, job.FileName), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDownload(ctx, job); err != nil {
		t.Fatal(err)
	}
	h := New(nil, nil, nil, Options{Downloads: service})
	send := func(method, suffix, rang string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1/api/downloads/"+job.ID+suffix, nil)
		if rang != "" {
			r.Header.Set("Range", rang)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := send("GET", "/media", "")
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), data) || w.Header().Get("Content-Type") != "video/mp4" || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "inline;") || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("media contract: %d %v", w.Code, w.Header())
	}
	w = send("GET", "/media", "bytes=4-13")
	if w.Code != 206 || !bytes.Equal(w.Body.Bytes(), data[4:14]) || !strings.HasPrefix(w.Header().Get("Content-Range"), "bytes 4-13/") {
		t.Fatal("seek range invalid")
	}
	// A real HTTP server proves file streaming can outlive its short API deadline.
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(delayedMediaWriter{w}, r) }))
	server.Config.WriteTimeout = 5 * time.Millisecond
	server.Start()
	response, err := server.Client().Get(server.URL + "/api/downloads/" + job.ID + "/media")
	if err != nil {
		server.Close()
		t.Fatal("API deadline interrupted media", err)
	}
	streamed, err := io.ReadAll(response.Body)
	response.Body.Close()
	server.Close()
	if err != nil || !bytes.Equal(streamed, data) {
		t.Fatal("media did not finish beyond API deadline")
	}
	w = send("HEAD", "/media", "")
	if w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Content-Length") == "" {
		t.Fatal("HEAD served body or lost metadata")
	}
	if w := send("GET", "/media", "bytes=99999-"); w.Code != 416 {
		t.Fatal("unsatisfiable range not reported")
	}
	if w := send("GET", "/file", ""); w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatal("file download behavior changed")
	}
	for _, host := range []string{"evil.test", "127.0.0.1"} {
		r := httptest.NewRequest("GET", "http://"+host+"/api/downloads/"+job.ID+"/media", nil)
		if host == "127.0.0.1" {
			r.Header.Set("Origin", "https://evil.test")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("media escaped local access guard")
		}
	}
	job.State = "queued"
	store.SaveDownload(ctx, job)
	if w := send("GET", "/media", ""); w.Code != 404 {
		t.Fatal("unfinished file was streamed")
	}
	job.State = "completed"
	job.FileName = "preview.html"
	store.SaveDownload(ctx, job)
	os.WriteFile(filepath.Join(directory, job.FileName), []byte("<script>not media</script>"), 0600)
	if w := send("GET", "/media", ""); w.Code != 404 {
		t.Fatal("HTML streamed inline")
	}
	job.FileName = "video.mp4"
	store.SaveDownload(ctx, job)
	os.Remove(filepath.Join(directory, job.FileName))
	if w := send("GET", "/media", ""); w.Code != 404 {
		t.Fatal("missing file reported playable")
	}
}

func TestDownloadHistoryDeleteHTTPContract(t *testing.T) {
	root := t.TempDir()
	store, err := sqlite.Open(filepath.Join(root, "delete-http.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := downloads.NewService(store, inactiveMediaRunner{}, filepath.Join(root, "media"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	ctx := context.Background()
	job := downloads.Job{ID: "http-delete-job", URL: "https://www.bilibili.com/video/BV17x411w7KC/", State: "completed", FileName: "video.mp4", CreatedAt: 1}
	if err := store.SaveDownload(ctx, job); err != nil {
		t.Fatal(err)
	}
	h := New(nil, nil, nil, Options{Downloads: service})
	request := httptest.NewRequest(http.MethodDelete, "http://127.0.0.1/api/downloads/"+job.ID, strings.NewReader(`{"deleteFiles":false}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("delete status: %d %s", response.Code, response.Body.String())
	}
	if _, err := store.GetDownload(ctx, job.ID); !errors.Is(err, downloads.ErrNotFound) {
		t.Fatalf("download record retained: %v", err)
	}
	active := downloads.Job{ID: "http-active-job", URL: "https://www.bilibili.com/video/BV1xx411c7mD", State: "running", FileName: "video.mp4", CreatedAt: 2}
	if err := store.SaveDownload(ctx, active); err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodDelete, "http://127.0.0.1/api/downloads/"+active.ID, strings.NewReader(`{"deleteFiles":false}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "先取消") {
		t.Fatalf("active delete contract: %d %s", response.Code, response.Body.String())
	}
}
