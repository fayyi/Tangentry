package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadPasswordGeneratesOnceAndPrefersConfiguration(t *testing.T) {
	data := t.TempDir()
	first, generated, err := LoadPassword(data, "")
	if err != nil || !generated || len(first) < 20 {
		t.Fatalf("password not generated: %q %v %v", first, generated, err)
	}
	if info, err := os.Stat(filepath.Join(data, "admin-password")); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("stored password is not private", err)
	}
	again, generated, err := LoadPassword(data, "")
	if err != nil || generated || again != first {
		t.Fatal("stored password was not reused", err)
	}
	if configured, _, _ := LoadPassword(data, "from-env"); configured != "from-env" {
		t.Fatal("configured password ignored")
	}
}

func TestPasswordGateRequiresSignInAndSameOrigin(t *testing.T) {
	var forwarded *http.Request
	gate := NewPasswordGate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded = r
		w.WriteHeader(204)
	}), "correct horse")
	now := time.Unix(1_800_000_000, 0)
	gate.now = func() time.Time { return now }
	serve := func(method, target, origin, cookie string, body url.Values) *httptest.ResponseRecorder {
		var request *http.Request
		if body != nil {
			request = httptest.NewRequest(method, target, strings.NewReader(body.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			request = httptest.NewRequest(method, target, nil)
		}
		request.Header.Set("Origin", origin)
		if cookie != "" {
			request.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
		}
		response := httptest.NewRecorder()
		gate.ServeHTTP(response, request)
		return response
	}
	const site = "http://media.example:7380"
	if response := serve("GET", site+"/healthz", "", "", nil); response.Code != 204 {
		t.Fatal("health check requires a session")
	}
	if response := serve("GET", site+"/api/library", "", "", nil); response.Code != 401 || forwarded != nil {
		t.Fatal("API admitted without a session")
	}
	if response := serve("GET", site+"/", "", "", nil); response.Code != 200 || !strings.Contains(response.Body.String(), `action="/login"`) {
		t.Fatal("app shown without a session")
	}
	if response := serve("POST", site+"/login", "https://attacker.example", "", url.Values{"password": {"correct horse"}}); response.Code != 403 {
		t.Fatal("cross-origin sign-in accepted")
	}
	if response := serve("POST", site+"/login", site, "", url.Values{"password": {"wrong"}}); response.Code != 401 || len(response.Result().Cookies()) != 0 {
		t.Fatal("wrong password accepted")
	}
	response := serve("POST", site+"/login", site, "", url.Values{"password": {"correct horse"}})
	cookies := response.Result().Cookies()
	if response.Code != http.StatusSeeOther || len(cookies) != 1 {
		t.Fatal("sign-in failed", response.Code)
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Secure {
		t.Fatalf("session cookie attributes: %+v", cookie)
	}
	if response := serve("POST", site+"/api/notes", "https://attacker.example", cookie.Value, nil); response.Code != 403 || forwarded != nil {
		t.Fatal("foreign origin admitted with a session")
	}
	if response := serve("POST", site+"/api/notes", "https://media.example:7380", cookie.Value, nil); response.Code != 204 {
		t.Fatal("proxied https origin rejected", response.Code)
	}
	if forwarded.Host != "localhost" || forwarded.Header.Get("Origin") != "http://localhost" {
		t.Fatalf("request not normalized: %s %s", forwarded.Host, forwarded.Header.Get("Origin"))
	}
	now = now.Add(sessionLifetime + time.Minute)
	if response := serve("GET", site+"/api/library", "", cookie.Value, nil); response.Code != 401 {
		t.Fatal("expired session admitted")
	}
}

func TestPasswordGateMarksSessionSecureBehindHTTPS(t *testing.T) {
	gate := NewPasswordGate(http.NotFoundHandler(), "pw")
	request := httptest.NewRequest("POST", "http://media.example/login", strings.NewReader("password=pw"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("X-Forwarded-Proto", "https")
	response := httptest.NewRecorder()
	gate.ServeHTTP(response, request)
	if cookies := response.Result().Cookies(); len(cookies) != 1 || !cookies[0].Secure {
		t.Fatal("session cookie not marked Secure behind HTTPS")
	}
}
