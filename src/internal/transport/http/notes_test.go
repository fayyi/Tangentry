package httptransport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/notes"
)

func TestNoteHTTPRoundTripAndInputBoundaries(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	h := New(nil, nil, nil, Options{Notes: notes.NewService(store)})
	send := func(method, path, body, contentType string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1"+path, strings.NewReader(body))
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	const valid = `{"contentId":"bilibili:BV17x411w7KC","body":"第一条笔记","positionSeconds":0}`
	w := send("POST", "/api/notes", valid, "application/json; charset=utf-8")
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var note notes.Note
	if err := json.Unmarshal(w.Body.Bytes(), &note); err != nil || note.ID == "" || note.PositionSeconds == nil || *note.PositionSeconds != 0 {
		t.Fatal("invalid created note contract")
	}
	w = send("GET", "/api/notes?contentId=bilibili%3ABV17x411w7KC", "", "")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || !bytes.Contains(w.Body.Bytes(), []byte(note.ID)) {
		t.Fatal("note list not available")
	}
	w = send("PATCH", "/api/notes/"+note.ID, `{"body":"edited","positionSeconds":null}`, "application/json")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"positionSeconds":null`) {
		t.Fatal("edit failed")
	}
	path := "/api/notes/" + note.ID + "/pin"
	w = send("PUT", path, `{"pinned":true}`, "application/json")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"pinned":true`) {
		t.Fatal("pin failed", w.Code, w.Body.String())
	}
	if result := send("GET", "/api/notes/search?pinnedOnly=true", "", ""); result.Code != 200 || !strings.Contains(result.Body.String(), note.ID) {
		t.Fatal("pinned search failed")
	}
	for _, query := range []string{"pinnedOnly=1", "pinnedOnly=", "pinnedOnly=true&pinnedOnly=false"} {
		if result := send("GET", "/api/notes/search?"+query, "", ""); result.Code != 400 {
			t.Fatal("invalid pin filter accepted", query)
		}
	}
	for _, body := range []string{`{}`, `{"pinned":null}`, `{"pinned":"true"}`, `{"pinned":true,"body":"overwrite"}`} {
		if result := send("PUT", path, body, "application/json"); result.Code != 400 {
			t.Fatal("invalid pin accepted", body, result.Code)
		}
	}
	w = send("PUT", path, `{"pinned":false}`, "application/json")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"pinned":false`) {
		t.Fatal("unpin failed")
	}
	for _, body := range []string{
		`{"contentId":"bilibili:BV17x411w7KC","body":" "}`,
		`{"contentId":"bilibili:BV17x411w7KC","body":"time","positionSeconds":-1}`,
		valid + `{}`,
		`{"contentId":"bilibili:BV17x411w7KC","body":"valid","unknown":"private"}`,
	} {
		if w := send("POST", "/api/notes", body, "application/json"); w.Code != 400 || strings.Contains(w.Body.String(), "private") {
			t.Fatalf("bad input accepted/leaked: %d %s", w.Code, w.Body.String())
		}
	}
	if w := send("PATCH", "/api/notes/"+note.ID, valid, "application/json"); w.Code != 400 {
		t.Fatal("note moved to a different content ID")
	}
	if w := send("POST", "/api/notes", valid, "text/plain"); w.Code != 415 {
		t.Fatal("non-JSON accepted")
	}
	if w := send("POST", "/api/notes", strings.Repeat(" ", 129<<10)+valid, "application/json"); w.Code != 400 {
		t.Fatal("oversized request accepted")
	}
	if w := send("GET", "/api/notes?contentId=invalid", "", ""); w.Code != 400 {
		t.Fatal("bad source accepted")
	}
	if w := send("DELETE", "/api/notes/"+note.ID, "", ""); w.Code != 204 {
		t.Fatal("delete failed")
	}
	if w := send("DELETE", "/api/notes/"+note.ID, "", ""); w.Code != 404 {
		t.Fatal("missing note not reported")
	}
	w = send("GET", "/api/notes?contentId=bilibili%3ABV17x411w7KC", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"notes":[]`) {
		t.Fatal("deleted note survived")
	}
}

func TestSubtitleReferenceHTTPAllowsCompleteUnicodeNotesAndRejectsMalformedSnapshots(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	handler := New(nil, nil, nil, Options{Notes: notes.NewService(store)})
	input := notes.Input{ContentID: "youtube:abcdefghijk", Body: strings.Repeat("😀\x01", 4000)}
	for i := 0; i < 4; i++ {
		input.SubtitleReferences = append(input.SubtitleReferences, notes.SubtitleReference{Start: float64(i), End: float64(i) + .5, Text: strings.Repeat("😀", 2000), Language: "zh"})
	}
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) <= 64<<10 {
		t.Fatal("fixture must exceed the former request limit")
	}
	send := func(body []byte) *httptest.ResponseRecorder {
		request := httptest.NewRequest("POST", "http://127.0.0.1/api/notes", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	response := send(body)
	var saved notes.Note
	if response.Code != 201 || json.Unmarshal(response.Body.Bytes(), &saved) != nil || len(saved.SubtitleReferences) != 4 || saved.Body != input.Body {
		t.Fatal("complete note rejected", response.Code)
	}
	response = send([]byte(`{"contentId":"youtube:abcdefghijk","body":"bad","subtitleReferences":[{"start":0,"end":1,"text":"quote","language":"en","unknown":true}]}`))
	if response.Code != 400 {
		t.Fatal("unknown snapshot field accepted", response.Code)
	}
}

func TestNoteHTTPVersionsRejectStaleAndMalformedRequests(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "versions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	h := New(nil, nil, nil, Options{Notes: notes.NewService(store)})
	send := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1"+path, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := send("POST", "/api/notes", `{"body":"original"}`)
	var original notes.Note
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &original) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	path := "/api/notes/" + original.ID
	w = send("PATCH", path, fmt.Sprintf(`{"body":"updated","expectedUpdatedAt":%d}`, original.UpdatedAt))
	var updated notes.Note
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &updated) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, method := range []string{"PATCH", "DELETE"} {
		requestPath, body := path, fmt.Sprintf(`{"body":"stale","expectedUpdatedAt":%d}`, original.UpdatedAt)
		if method == "DELETE" {
			requestPath += fmt.Sprintf("?expectedUpdatedAt=%d", original.UpdatedAt)
			body = ""
		}
		w = send(method, requestPath, body)
		if w.Code != 409 || !strings.Contains(w.Body.String(), `"code":"note_conflict"`) {
			t.Fatal(method, w.Code, w.Body.String())
		}
	}
	for _, query := range []string{"", "-1", "abc", "1.5", "9223372036854775808", "1&expectedUpdatedAt=2"} {
		if w := send("DELETE", path+"?expectedUpdatedAt="+query, ""); w.Code != 400 {
			t.Fatalf("accepted %q: %d", query, w.Code)
		}
	}
	w = send("DELETE", path+fmt.Sprintf("?expectedUpdatedAt=%d", updated.UpdatedAt), "")
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = send("DELETE", path+fmt.Sprintf("?expectedUpdatedAt=%d", updated.UpdatedAt), "")
	if w.Code != 404 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestYouTubeNoteHTTPKeepsIdentityAndSupportsSearch(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "youtube-notes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	h := New(nil, nil, nil, Options{Notes: notes.NewService(store)})
	send := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1"+path, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := send("POST", "/api/notes", `{"contentId":"youtube:jNQXAC9IVRw","body":"YouTube 隔离笔记","positionSeconds":1.25}`)
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var note notes.Note
	if json.Unmarshal(w.Body.Bytes(), &note) != nil || note.ContentID != "youtube:jNQXAC9IVRw" || note.PositionSeconds == nil || *note.PositionSeconds != 1.25 {
		t.Fatal("wrong YouTube contract")
	}
	w = send("GET", "/api/notes?contentId=youtube%3AjNQXAC9IVRw", "")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), note.ID) {
		t.Fatal("YouTube list unavailable")
	}
	w = send("GET", "/api/notes/search?q=YouTube", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), note.ID) {
		t.Fatal("YouTube search unavailable")
	}
	w = send("PATCH", "/api/notes/"+note.ID, `{"body":"moved","contentId":"bilibili:BV17x411w7KC"}`)
	if w.Code != 400 {
		t.Fatal("edit changed source")
	}
	w = send("PATCH", "/api/notes/"+note.ID, `{"body":"updated","positionSeconds":null}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"contentId":"youtube:jNQXAC9IVRw"`) {
		t.Fatal("lost source on edit")
	}
	w = send("GET", "/api/notes?contentId=bilibili%3ABV17x411w7KC", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"notes":[]`) {
		t.Fatal("YouTube notes leaked to Bilibili")
	}
	w = send("GET", "/api/notes?contentId=youtube%3Ainvalid", "")
	if w.Code != 400 {
		t.Fatal("invalid YouTube identity accepted")
	}
	w = send("DELETE", "/api/notes/"+note.ID, "")
	if w.Code != 204 {
		t.Fatal("YouTube delete failed")
	}
}
