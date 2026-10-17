package httptransport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/credentials"
	"tangentry/internal/subtitles"
)

func storageRequest(h http.Handler, method, path, body, media string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1"+path, strings.NewReader(body))
	if media != "" {
		r.Header.Set("Content-Type", media)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestCookieHTTPNeverReturnsSecretsAndRejectedWritesKeepState(t *testing.T) {
	store, err := credentials.Open(filepath.Join(t.TempDir(), "credentials"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	h := New(nil, nil, nil, Options{Cookies: store})
	secret := "synthetic-private-value"
	w := storageRequest(h, "PUT", "/api/settings/cookies/bilibili", `{"format":"header","value":"SESSDATA=`+secret+`"}`, "application/json")
	if w.Code != 200 || strings.Contains(w.Body.String(), secret) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unsafe save response: %d %s", w.Code, w.Body)
	}
	for _, tc := range []struct {
		platform, body, media string
		status                int
	}{
		{"bilibili", `{"format":"header","value":"SID=bad","extra":true}`, "application/json", 400},
		{"bilibili", `{"format":"header","value":"SID=bad"} {}`, "application/json", 400},
		{"bilibili", `{"format":"netscape","value":".youtube.com\tTRUE\t/\tTRUE\t0\tSID\tbad"}`, "application/json", 400},
		{"bilibili", `{"format":"header","value":"SID=bad"}`, "text/plain", 415},
		{"unknown", `{"format":"header","value":"SID=bad"}`, "application/json", 400},
	} {
		w = storageRequest(h, "PUT", "/api/settings/cookies/"+tc.platform, tc.body, tc.media)
		if w.Code != tc.status || strings.Contains(w.Body.String(), secret) {
			t.Fatalf("bad rejected response: %d %s", w.Code, w.Body)
		}
		if store.Header("bilibili", "https://www.bilibili.com/") != "SESSDATA="+secret {
			t.Fatal("failed save replaced credential")
		}
	}
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		r := httptest.NewRequest(method, "http://127.0.0.1/api/settings/cookies/bilibili", strings.NewReader(`{"format":"header","value":"SID=bad"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://untrusted.example")
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("external credential access permitted")
		}
	}
	w = storageRequest(h, "GET", "/api/settings/cookies", "", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), secret) {
		t.Fatal("secret leaked in status")
	}
	w = storageRequest(h, "DELETE", "/api/settings/cookies/bilibili", "", "")
	if w.Code != 200 || store.Header("bilibili", "https://www.bilibili.com/") != "" {
		t.Fatal("clear did not select anonymous")
	}
}

func TestSavedSubtitleHTTPStrictValidationAndVersionConflicts(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	h := New(nil, nil, nil, Options{SavedSubtitles: subtitles.NewSavedService(store)})
	path := "/api/saved-subtitles/bilibili:BV17x411w7KC/zh"
	valid := `{"revision":0,"track":{"language":"zh","label":"中文","automatic":false,"cues":[{"start":0,"end":1,"text":"字幕"}]}}`
	for _, body := range []string{
		strings.Replace(valid, `"revision":0`, `"revision":null`, 1),
		strings.Replace(valid, `"automatic":false,`, "", 1),
		strings.Replace(valid, `"end":1`, `"end":0`, 1),
		strings.Replace(valid, `"start":0,`, "", 1),
		strings.Replace(valid, `"language":"zh"`, `"language":"en"`, 1),
		strings.Replace(valid, `"label":"中文"`, `"label":""`, 1),
		strings.Replace(valid, `"revision":0`, `"revision":0,"unexpected":true`, 1),
		valid + ` {}`,
	} {
		w := storageRequest(h, "PUT", path, body, "application/json")
		if w.Code != 400 {
			t.Fatalf("invalid subtitle accepted: %d %s", w.Code, w.Body)
		}
	}
	if w := storageRequest(h, "PUT", path, valid, "text/plain"); w.Code != 415 {
		t.Fatal("wrong content type accepted")
	}
	if w := storageRequest(h, "PUT", path, valid, "application/json"); w.Code != 200 || !strings.Contains(w.Body.String(), `"revision":1`) {
		t.Fatalf("save failed: %d %s", w.Code, w.Body)
	}
	if w := storageRequest(h, "PUT", path, valid, "application/json"); w.Code != 409 {
		t.Fatal("stale save overwrote subtitle")
	}
	for _, revision := range []string{"", "0", "invalid", "2"} {
		w := storageRequest(h, "DELETE", path+"?revision="+revision, "", "")
		if w.Code != 400 && w.Code != 409 {
			t.Fatal("invalid delete revision accepted")
		}
	}
	if tracks, _ := store.ListSavedSubtitles(context.Background(), "bilibili:BV17x411w7KC"); len(tracks) != 1 {
		t.Fatal("rejected delete removed subtitle")
	}
	if w := storageRequest(h, "GET", "/api/saved-subtitles?contentId=bilibili:BV17x411w7KC", "", ""); w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("list contract failed")
	}
	if w := storageRequest(h, "DELETE", path+"?revision=1", "", ""); w.Code != 204 {
		t.Fatal("confirmed delete failed")
	}
	if w := storageRequest(h, "GET", "/api/saved-subtitles?contentId=bilibili:BV17x411w7KC", "", ""); !strings.Contains(w.Body.String(), `"tracks":[]`) {
		t.Fatal("empty list not normalized")
	}
	if w := storageRequest(h, "PUT", path, valid, "application/json"); w.Code != 200 || !strings.Contains(w.Body.String(), `"revision":3`) {
		t.Fatal("recreation reset monotonic revision")
	}
	if w := storageRequest(h, "PUT", path, strings.Replace(valid, `"revision":0`, `"revision":1`, 1), "application/json"); w.Code != 409 {
		t.Fatal("old saved revision overwrote recreated subtitle")
	}
}
