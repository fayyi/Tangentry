package httptransport

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/readingprogress"
	"testing"
)

func TestProgressBatchHTTP(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "batch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := readingprogress.NewService(store)
	if _, err := service.Save(context.Background(), "bilibili:BV17x411w7KC", readingprogress.Input{PositionSeconds: 4, DurationSeconds: 12}); err != nil {
		t.Fatal(err)
	}
	h := New(nil, nil, nil, Options{Progress: service})
	for _, test := range []struct {
		query  string
		status int
		count  int
	}{
		{"contentId=bilibili%3ABV17x411w7KC&contentId=bilibili%3ABVresume0000&contentId=bilibili%3ABV17x411w7KC", 200, 1},
		{"contentId=bilibili%3ABVresume0000", 200, 0},
		{"", 400, 0}, {"contentId=invalid", 400, 0}, {strings.Repeat("contentId=bilibili%3ABV17x411w7KC&", 101), 400, 0},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1/api/reading-progress/batch?"+test.query, nil))
		if w.Code != test.status {
			t.Fatalf("batch status: %d %s", w.Code, w.Body.String())
		}
		if test.status == 200 {
			var result struct {
				Items []readingprogress.Progress `json:"items"`
			}
			if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Items == nil || len(result.Items) != test.count {
				t.Fatal("wrong batch response")
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("batch cached")
			}
		}
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "http://127.0.0.1/api/reading-progress/batch?contentId=bilibili%3ABV17x411w7KC", nil)
	r.Header.Set("Origin", "https://elsewhere.example")
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("batch source constraint lost")
	}
	w = httptest.NewRecorder()
	New(nil, nil, nil).ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1/api/reading-progress/batch?contentId=bilibili%3ABV17x411w7KC", nil))
	if w.Code != 503 {
		t.Fatal("unconfigured batch service succeeded")
	}
}

func TestProgressHTTPContractAndRequiredFields(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "progress.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	h := New(nil, nil, nil, Options{Progress: readingprogress.NewService(store)})
	send := func(method, path, body, mime string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1"+path, strings.NewReader(body))
		if mime != "" {
			r.Header.Set("Content-Type", mime)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	path := "/api/reading-progress/bilibili:BV17x411w7KC"
	if w := send("GET", "/api/reading-progress?contentId=bilibili%3ABV17x411w7KC", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"progress":null`) {
		t.Fatal("unknown progress should be null")
	}
	w := send("PUT", path, `{"positionSeconds":5,"durationSeconds":12,"revision":0}`, "application/json; charset=utf-8")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("save failed: %d %s", w.Code, w.Body.String())
	}
	var value readingprogress.Progress
	json.Unmarshal(w.Body.Bytes(), &value)
	if value.Revision != 1 || value.PositionSeconds != 5 || value.Completed {
		t.Fatal("invalid contract")
	}
	if w := send("PUT", path, `{"positionSeconds":0,"durationSeconds":12,"revision":0}`, "application/json"); w.Code != 409 || !strings.Contains(w.Body.String(), "progress_conflict") {
		t.Fatal("stale write not rejected")
	}
	for _, body := range []string{
		`{}`, `{"positionSeconds":null,"durationSeconds":12,"revision":1}`, `{"positionSeconds":0,"durationSeconds":12}`, `{"positionSeconds":5,"durationSeconds":null,"revision":1}`,
		`{"positionSeconds":0,"durationSeconds":12,"revision":null}`, `{"positionSeconds":5,"durationSeconds":12,"revision":1.5}`, `{"positionSeconds":5,"durationSeconds":12,"revision":1,"unknown":"secret"}`,
		`{"positionSeconds":13,"durationSeconds":12,"revision":1}`, `{"positionSeconds":0,"durationSeconds":12,"revision":1}{}`, strings.Repeat(" ", 5<<10) + `{}`,
	} {
		if w := send("PUT", path, body, "application/json"); w.Code != 400 || strings.Contains(w.Body.String(), "secret") {
			t.Fatalf("invalid input accepted: %d", w.Code)
		}
	}
	if w := send("PUT", path, `{}`, "text/plain"); w.Code != 415 {
		t.Fatal("mime not checked")
	}
	if w := send("GET", "/api/reading-progress?contentId=invalid", "", ""); w.Code != 400 {
		t.Fatal("invalid source accepted")
	}
	w = send("GET", "/api/reading-progress?contentId=bilibili%3ABV17x411w7KC", "", "")
	if !strings.Contains(w.Body.String(), `"positionSeconds":5`) || !strings.Contains(w.Body.String(), `"revision":1`) {
		t.Fatal("failure changed stored progress")
	}
}
