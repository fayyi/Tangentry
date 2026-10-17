package bilibili

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestMergeCookiesRetainsUnchangedSessionFields(t *testing.T) {
	got := mergeCookies("SESSDATA=old; bili_jct=csrf; DedeUserID=42; buvid3=old-buvid", "SESSDATA=new; bili_jct=new-csrf")
	for _, want := range []string{"SESSDATA=new", "bili_jct=new-csrf", "DedeUserID=42", "buvid3=old-buvid"} {
		if !strings.Contains(got, want) {
			t.Fatalf("merged cookie missing %q: %s", want, got)
		}
	}
}

func TestCredentialRefresherProtocolPersistsBeforeConfirmation(t *testing.T) {
	sequence := []string{}
	var saved bool
	transport := loginTransport(func(req *http.Request) (*http.Response, error) {
		sequence = append(sequence, req.URL.Host+req.URL.Path)
		switch {
		case req.URL.Host == "passport.bilibili.com" && strings.HasSuffix(req.URL.Path, "/cookie/info"):
			return response(`{"code":0,"data":{"refresh":true}}`), nil
		case req.URL.Host == "www.bilibili.com" && strings.HasPrefix(req.URL.Path, "/correspond/1/"):
			return response(`<div id="1-name">refresh-csrf</div>`), nil
		case req.URL.Host == "passport.bilibili.com" && strings.HasSuffix(req.URL.Path, "/cookie/refresh"):
			if got := req.Header.Get("Cookie"); got != "SESSDATA=old; bili_jct=old-csrf; DedeUserID=7" {
				t.Fatalf("old cookie not sent: %s", got)
			}
			body, _ := io.ReadAll(req.Body)
			form, _ := url.ParseQuery(string(body))
			if form.Get("refresh_token") != "old-refresh" || form.Get("refresh_csrf") != "refresh-csrf" {
				t.Fatalf("invalid refresh form: %s", body)
			}
			res := response(`{"code":0,"data":{"refresh_token":"new-refresh"}}`)
			res.Header.Add("Set-Cookie", "SESSDATA=new; Domain=.bilibili.com; Path=/")
			res.Header.Add("Set-Cookie", "bili_jct=new-csrf; Domain=.bilibili.com; Path=/")
			return res, nil
		case req.URL.Host == "passport.bilibili.com" && strings.HasSuffix(req.URL.Path, "/confirm/refresh"):
			if !saved {
				t.Fatal("confirmation ran before credentials were persisted")
			}
			body, _ := io.ReadAll(req.Body)
			form, _ := url.ParseQuery(string(body))
			if form.Get("refresh_token") != "old-refresh" || form.Get("csrf") != "new-csrf" {
				t.Fatalf("invalid confirmation form: %s", body)
			}
			return response(`{"code":0}`), nil
		default:
			t.Fatalf("unexpected request %s", req.URL.String())
			return nil, nil
		}
	})
	refresher := &CredentialRefresher{
		HTTP: &http.Client{Transport: transport},
		Load: func() (string, string) { return "SESSDATA=old; bili_jct=old-csrf; DedeUserID=7", "old-refresh" },
		Save: func(oldCookie, oldRefresh, newCookie, newRefresh string) error {
			if oldCookie == newCookie || oldRefresh == newRefresh || !strings.Contains(newCookie, "DedeUserID=7") {
				t.Fatalf("unexpected rotated credentials: %s %s", newCookie, newRefresh)
			}
			saved = true
			return nil
		},
	}
	if err := refresher.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(sequence) != 4 || !strings.HasSuffix(sequence[3], "/confirm/refresh") {
		t.Fatalf("unexpected protocol sequence: %#v", sequence)
	}
}

func response(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
