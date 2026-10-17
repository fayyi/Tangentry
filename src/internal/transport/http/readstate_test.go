package httptransport

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/readstate"
	"testing"
)

func TestReadStateHTTPBoundaries(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "http.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	h := New(nil, nil, nil, Options{ReadState: readstate.New(store)})
	send := func(method, path, body, kind string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1"+path, strings.NewReader(body))
		if kind != "" {
			r.Header.Set("Content-Type", kind)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	id := "youtube:dQw4w9WgXcQ"
	if w := send("GET", "/api/read-state?contentId="+id, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"read":false`) {
		t.Fatal("missing state default")
	}
	w := send("PUT", "/api/read-state", `{"contentIds":["`+id+`","`+id+`"],"read":true}`, "application/json; charset=utf-8")
	var result struct {
		States []readstate.State `json:"states"`
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || len(result.States) != 1 || !result.States[0].Read {
		t.Fatalf("write: %d %s", w.Code, w.Body.String())
	}
	for _, body := range []string{`{"contentIds":["` + id + `"]}`, `{"contentIds":["` + id + `"],"read":null}`, `{"contentIds":["` + id + `","wrong"],"read":false}`, `{"contentIds":[],"read":false}`, `{"contentIds":["` + id + `"],"read":false,"secret":"hidden"}`, `{"contentIds":["` + id + `"],"read":false}{}`, strings.Repeat(" ", 33<<10) + `{}`} {
		if w := send("PUT", "/api/read-state", body, "application/json"); w.Code != 400 || strings.Contains(w.Body.String(), "hidden") {
			t.Fatal("invalid input accepted or leaked")
		}
	}
	if w := send("PUT", "/api/read-state", `{}`, "text/plain"); w.Code != 415 {
		t.Fatal("MIME boundary")
	}
	if w := send("GET", "/api/read-state?"+strings.Repeat("contentId="+id+"&", 201), "", ""); w.Code != 400 {
		t.Fatal("batch unbounded")
	}
	r := httptest.NewRequest("PUT", "http://127.0.0.1/api/read-state", strings.NewReader(`{"contentIds":["`+id+`"],"read":false}`))
	r.Header.Set("Origin", "https://unrelated.example")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("origin constraint lost")
	}
	states, _ := store.ListReadStates(context.Background(), []string{id})
	if !states[0].Read {
		t.Fatal("invalid request changed previous state")
	}
	w = httptest.NewRecorder()
	New(nil, nil, nil).ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1/api/read-state?contentId="+id, nil))
	if w.Code != 503 {
		t.Fatal("missing service")
	}
}
