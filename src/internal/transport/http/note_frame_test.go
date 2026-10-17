package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/jpeg"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/notes"
)

func TestNoteFrameHTTPRequiresExplicitInputsAndGuardsPrivateImages(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "frame.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	position := 2.0
	note, _ := notes.NewService(store).Create(context.Background(), notes.Input{ContentID: "youtube:abcdefghijk", Body: "测试", PositionSeconds: &position})
	source := filepath.Join(t.TempDir(), "video")
	os.WriteFile(source, []byte("fixture"), 0600)
	var jpegData bytes.Buffer
	jpeg.Encode(&jpegData, image.NewRGBA(image.Rect(0, 0, 16, 9)), nil)
	frames := notes.NewFrames(store, func(context.Context, string, string) (*os.File, error) { return os.Open(source) }, func(context.Context, string, float64) ([]byte, error) { return jpegData.Bytes(), nil })
	handler := New(nil, nil, nil, Options{NoteFrames: frames})
	send := func(method, path, body, origin string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, "http://127.0.0.1"+path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", origin)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	for _, path := range []string{"/api/notes/" + note.ID + "/frame", "/api/notes/" + note.ID + "/frames"} {
		for _, body := range []string{`{}`, `{"downloadId":"` + strings.Repeat("a", 32) + `","expectedUpdatedAt":1}`, `{"downloadId":"../../etc/passwd","positionSeconds":2,"expectedUpdatedAt":1}`, `{"positionSeconds":2,"expectedUpdatedAt":1,"url":"file:///etc/passwd"}`} {
			if response := send("POST", path, body, "http://127.0.0.1"); response.Code != 400 {
				t.Fatal(path, response.Code, response.Body.String())
			}
		}
	}
	path := "/api/notes/" + note.ID + "/frame"
	data, _ := json.Marshal(notes.FrameInput{DownloadID: strings.Repeat("a", 32), PositionSeconds: 2, ExpectedUpdatedAt: note.UpdatedAt})
	if response := send("POST", path, string(data), "https://evil.example"); response.Code != 403 {
		t.Fatal("cross-origin mutation", response.Code)
	}
	response := send("POST", path, string(data), "http://127.0.0.1")
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var saved notes.Note
	if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil || saved.Frame == nil {
		t.Fatal("frame response", err)
	}
	imagePath := "/api/notes/" + note.ID + "/frames/" + saved.Frame.ID
	response = send("GET", imagePath, "", "http://127.0.0.1")
	if response.Code != 200 || response.Header().Get("Content-Type") != "image/jpeg" || response.Header().Get("Cache-Control") != "private, no-store" || !bytes.Equal(response.Body.Bytes(), jpegData.Bytes()) {
		t.Fatal(response.Code, response.Header())
	}
	if response := send("GET", "/api/notes/"+strings.Repeat("e", 32)+"/frames/"+saved.Frame.ID, "", "http://127.0.0.1"); response.Code != 404 {
		t.Fatal("other note image", response.Code)
	}
	creation := notes.FrameNoteInput{Note: notes.Input{CreateID: strings.Repeat("f", 32), ContentID: note.ContentID, Body: "一起保存", PositionSeconds: &position}, Capture: notes.FrameSpec{DownloadID: strings.Repeat("a", 32), PositionSeconds: &position}}
	paired, _ := json.Marshal(creation)
	for _, body := range []string{`{}`, `{"note":{"body":"missing identity"},"capture":{}}`, `{"note":{},"capture":{"url":"file:///etc/passwd"}}`} {
		if response := send("POST", "/api/notes/with-frame", body, "http://127.0.0.1"); response.Code != 400 {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	if response := send("POST", "/api/notes/with-frame", string(paired), "https://evil.example"); response.Code != 403 {
		t.Fatal("cross-origin create", response.Code)
	}
	response = send("POST", "/api/notes/with-frame", string(paired), "http://127.0.0.1")
	if response.Code != 201 {
		t.Fatal(response.Code, response.Body.String())
	}
	var created notes.Note
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil || created.Frame == nil {
		t.Fatal("paired response", err)
	}
	response = send("POST", "/api/notes/with-frame", string(paired), "http://127.0.0.1")
	var retry notes.Note
	if err := json.Unmarshal(response.Body.Bytes(), &retry); err != nil || response.Code != 201 || retry.ID != created.ID || retry.Frame.ID != created.Frame.ID {
		t.Fatal("retry", response.Code, err)
	}
	creation.Note.Body = "不同正文"
	paired, _ = json.Marshal(creation)
	response = send("POST", "/api/notes/with-frame", string(paired), "http://127.0.0.1")
	if response.Code != 409 || !strings.Contains(response.Body.String(), "note_creation_conflict") {
		t.Fatal(response.Code, response.Body.String())
	}
}
