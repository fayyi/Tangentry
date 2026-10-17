package server

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"tangentry/internal/app"
)

func TestLoopbackRequiresSessionAndRejectsForeignOrigins(t *testing.T) {
	application, err := app.New(t.TempDir(), fstest.MapFS{"index.html": {Data: []byte("desktop")}})
	if err != nil {
		t.Fatal(err)
	}
	backend := New(application.RuntimePort(), application.ShellStatePort(), ShellOptions{PickPath: func(_ context.Context, kind, current string) (string, error) { return "", nil }})
	defer backend.Close()
	address, stop, err := backend.StartLoopback()
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	u, _ := url.Parse(address)
	root := u.Scheme + "://" + u.Host
	response, err := http.Get(root + "/api/desktop/download-directory")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("untrusted local process admitted")
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	response, err = client.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("bootstrap failed", response.StatusCode)
	}
	request, _ := http.NewRequest("POST", root+"/api/desktop/select-path", strings.NewReader(`{"kind":"evil"}`))
	request.Header.Set("Origin", "https://attacker.example")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("foreign origin admitted")
	}
	request, _ = http.NewRequest("POST", root+"/api/desktop/select-path", strings.NewReader(`{"kind":"whisper-model"}`))
	request.Header.Set("Origin", root)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("trusted picker rejected", response.StatusCode)
	}
}
