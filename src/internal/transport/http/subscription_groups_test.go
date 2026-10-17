package httptransport

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/content"
	"tangentry/internal/subscriptions"
	"testing"
)

func TestSubscriptionGroupsHTTPExplicitChoicesAndConflicts(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "groups.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sources := subscriptions.NewService(store, nil)
	defer sources.Close()
	source, _, err := sources.Create(context.Background(), subscriptions.Input{Query: "12345"})
	if err != nil {
		t.Fatal(err)
	}
	h := New(content.NewService(store), nil, nil, Options{Subscriptions: sources, SubscriptionGroups: subscriptions.NewGroupService(store)})
	send := func(method, path, body, mime, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1"+path, strings.NewReader(body))
		if mime != "" {
			r.Header.Set("Content-Type", mime)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	base := "/api/subscription-groups"
	w := send("POST", base, `{"name":"学习"}`, "application/json", "")
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct{ Group subscriptions.Group }
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	path := base + "/" + result.Group.ID
	assign := "/api/subscriptions/" + source.ID + "/group"
	for _, body := range []string{`{}`, `{"groupId":null,"expectedGroupId":""}`, `{"groupId":"","expectedGroupId":null}`, `{"groupId":""}`, `{"groupId":"invalid","expectedGroupId":""}`, `{"groupId":"","expectedGroupId":"","secret":"private"}`, `{"groupId":"","expectedGroupId":""}{}`} {
		w = send("PUT", assign, body, "application/json", "")
		if w.Code != 400 || strings.Contains(w.Body.String(), "private") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	body := `{"groupId":"` + result.Group.ID + `","expectedGroupId":""}`
	if w = send("PUT", assign, body, "text/plain", ""); w.Code != 415 {
		t.Fatal("mime", w.Code)
	}
	if w = send("PUT", assign, body, "application/json", "https://evil.example"); w.Code != 403 {
		t.Fatal("origin", w.Code)
	}
	if w = send("PUT", assign, body, "application/json", ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = send("PUT", assign, body, "application/json", ""); w.Code != 409 {
		t.Fatal("stale move", w.Code)
	}
	if w = send("GET", "/api/subscriptions", "", "", ""); !strings.Contains(w.Body.String(), `"groupId":"`+result.Group.ID+`"`) {
		t.Fatal("membership not exposed", w.Body.String())
	}
	if w = send("PATCH", path, `{"name":"学习","revision":1}`, "application/json", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"revision":1`) {
		t.Fatal("idempotent rename", w.Code)
	}
	if w = send("PATCH", path, `{"name":"阅读","revision":1}`, "application/json", ""); w.Code != 200 {
		t.Fatal("rename", w.Code)
	}
	if w = send("DELETE", path, `{"revision":1}`, "application/json", ""); w.Code != 409 {
		t.Fatal("stale dissolution", w.Code)
	}
	if w = send("DELETE", path, `{}`, "application/json", ""); w.Code != 400 {
		t.Fatal("implicit revision", w.Code)
	}
	if w = send("DELETE", path, `{"revision":2}`, "application/json", ""); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = send("GET", "/api/subscriptions", "", "", ""); !strings.Contains(w.Body.String(), `"groupId":""`) || !strings.Contains(w.Body.String(), source.ID) {
		t.Fatal("dissolution removed source")
	}
	if w = send("GET", base, "", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"groups":[]`) {
		t.Fatal("empty contract")
	}
	for _, body := range []string{`{"name":"bad\nname"}`, `{"name":"name","revision":1}`, `{"name":"name"}{}`, strings.Repeat(" ", 9<<10) + `{"name":"name"}`} {
		if w = send("POST", base, body, "application/json", ""); w.Code != 400 {
			t.Fatal("invalid create", w.Code)
		}
	}
}
