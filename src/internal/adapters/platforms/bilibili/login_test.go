package bilibili

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type loginTransport func(*http.Request) (*http.Response, error)

func (f loginTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestQRLoginKeepsCookiesPrivateAndThrottles(t *testing.T) {
	polls, saves := 0, 0
	l := &QRLogin{HTTP: &http.Client{Transport: loginTransport(func(r *http.Request) (*http.Response, error) {
		body := `{"code":0,"data":{"url":"https://account.bilibili.com/h5/account-h5/auth/scan-web?qrcode_key=synthetic-key-123456","qrcode_key":"synthetic-key-123456"}}`
		header := http.Header{}
		if strings.Contains(r.URL.Path, "/poll") {
			polls++
			body = `{"code":0,"data":{"code":86090}}`
			if polls == 2 {
				body = `{"code":0,"data":{"code":0}}`
				header.Add("Set-Cookie", "SESSDATA=private-session; Domain=.bilibili.com; Path=/; Secure")
				header.Add("Set-Cookie", "bili_jct=private-csrf; Domain=.bilibili.com; Path=/; Secure")
			}
		}
		return &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}, Save: func(v string) error {
		saves++
		if !strings.Contains(v, "private-session") {
			t.Fatal("missing Cookie")
		}
		return nil
	}}
	s, err := l.Start(context.Background())
	if err != nil || !strings.HasPrefix(s.Image, "data:image/png;base64,") {
		t.Fatal(s, err)
	}
	s, err = l.Poll(context.Background(), s.ID)
	if err != nil || s.Status != "scanned" {
		t.Fatal(s, err)
	}
	_, _ = l.Poll(context.Background(), s.ID)
	if polls != 1 {
		t.Fatal("poll not throttled")
	}
	l.sessions[s.ID].next = time.Time{}
	s, err = l.Poll(context.Background(), s.ID)
	if err != nil || s.Status != "success" || saves != 1 || s.Image != "" {
		t.Fatal(s, err)
	}
	_, _ = l.Poll(context.Background(), s.ID)
	if saves != 1 {
		t.Fatal("saved twice")
	}
}

func TestQRLoginCapturesSaveBeforeGenerationAndKeepsRefreshPrivate(t *testing.T) {
	prepared, saved := false, false
	l := &QRLogin{PrepareSave: func() func(string, string) error {
		prepared = true
		return func(header, refresh string) error {
			if !strings.Contains(header, "private-session") || refresh != "private-refresh" {
				t.Fatal("credentials lost")
			}
			saved = true
			return nil
		}
	}, HTTP: &http.Client{Transport: loginTransport(func(r *http.Request) (*http.Response, error) {
		if !prepared {
			t.Fatal("snapshot captured too late")
		}
		body := `{"code":0,"data":{"url":"https://account.bilibili.com/h5/account-h5/auth/scan-web?qrcode_key=synthetic-key-123456","qrcode_key":"synthetic-key-123456"}}`
		header := http.Header{}
		if strings.Contains(r.URL.Path, "/poll") {
			body = `{"code":0,"data":{"code":0,"refresh_token":"private-refresh"}}`
			header.Add("Set-Cookie", "SESSDATA=private-session; Domain=.bilibili.com; Path=/; Secure")
			header.Add("Set-Cookie", "bili_jct=private-csrf; Domain=.bilibili.com; Path=/; Secure")
		}
		return &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	s, err := l.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s, err = l.Poll(context.Background(), s.ID)
	if err != nil || !saved || s.Status != "success" {
		t.Fatal(s, err)
	}
	raw, _ := json.Marshal(s)
	if strings.Contains(string(raw), "private-") {
		t.Fatal("secret exposed in login result")
	}
}
