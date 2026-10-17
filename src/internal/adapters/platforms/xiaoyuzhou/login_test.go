package xiaoyuzhou

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestLoginStartAndPollKeepsSuccess(t *testing.T) {
	var calls int
	var access, refresh string
	client := &http.Client{Transport: roundTripLogin(func(r *http.Request) (*http.Response, error) {
		calls++
		if strings.HasSuffix(r.URL.Path, "/create") {
			return loginResponse(`{"id":"6acd64c67c72d6f2749c917d","url":"https://h5.xiaoyuzhoufm.com/oauth?qrcode_id=6acd64c67c72d6f2749c917d"}`, nil), nil
		}
		if calls == 2 {
			h := http.Header{}
			h.Set("x-jike-access-token", "access-token-12345")
			h.Set("x-jike-refresh-token", "refresh-token-12345")
			return loginResponse(`{"status":"CONFIRMED"}`, h), nil
		}
		h := http.Header{}
		h.Set("x-jike-access-token", "access-token-12345")
		h.Set("x-jike-refresh-token", "refresh-token-12345")
		return loginResponse(`{"status":"CONFIRMED"}`, h), nil
	})}
	login := &Login{HTTP: client, Save: func(a, r string) error { access, refresh = a, r; return nil }}
	state, err := login.Start(context.Background())
	if err != nil || state.Status != "waiting" || state.Image == "" || len(state.ID) != 48 {
		t.Fatalf("start: %#v %v", state, err)
	}
	state, err = login.Poll(context.Background(), state.ID)
	if err != nil || state.Status != "success" || access != "access-token-12345" || refresh != "refresh-token-12345" {
		t.Fatalf("poll: %#v %v calls=%d", state, err, calls)
	}
	state, err = login.Poll(context.Background(), state.ID)
	if err != nil || state.Status != "success" {
		t.Fatalf("terminal poll: %#v %v", state, err)
	}
}

type roundTripLogin func(*http.Request) (*http.Response, error)

func (f roundTripLogin) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func loginResponse(body string, header http.Header) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: header}
}
