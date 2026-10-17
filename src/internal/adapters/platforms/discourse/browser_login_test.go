package discourse

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestBrowserLoginChecksEncryptedCallbackAndConfirmsSession(t *testing.T) {
	for _, scenario := range []string{"success", "wrong nonce", "blocked"} {
		t.Run(scenario, func(t *testing.T) {
			calls, saves := 0, 0
			login := &Login{HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Host != "linux.do" {
					t.Fatal("foreign login destination")
				}
				switch r.URL.Path {
				case "/session/csrf.json":
					if r.Header.Get("Cookie") != "" {
						t.Fatal("old credentials used in a new login")
					}
					if scenario == "blocked" {
						return response(r, 403, "blocked"), nil
					}
					result := response(r, 200, `{"csrf":"synthetic-csrf"}`)
					result.Header.Add("Set-Cookie", "_forum_session=synthetic-forum; Path=/; Secure; HttpOnly")
					return result, nil
				case "/session/otp/" + strings.Repeat("c", 32):
					if r.Method != "POST" || r.Header.Get("X-CSRF-Token") != "synthetic-csrf" || !strings.Contains(r.Header.Get("Cookie"), "_forum_session=synthetic-forum") {
						t.Fatal("missing CSRF/session")
					}
					result := response(r, 302, "")
					result.Header.Set("Location", "/")
					result.Header.Add("Set-Cookie", "_t=synthetic-login; Path=/; Secure; HttpOnly")
					return result, nil
				case "/session/current.json":
					if !strings.Contains(r.Header.Get("Cookie"), "_t=synthetic-login") {
						t.Fatal("login not confirmed")
					}
					return response(r, 200, `{"current_user":{"id":42,"username":"阅读者"}}`), nil
				default:
					t.Fatal("unexpected login endpoint")
				}
				return nil, nil
			})}, PrepareCookieSave: func() func([]*http.Cookie) error {
				return func(cookies []*http.Cookie) error {
					saves++
					found := false
					for _, cookie := range cookies {
						found = found || cookie.Name == "_t" && cookie.Value == "synthetic-login"
					}
					if !found {
						t.Fatal("missing login cookie")
					}
					return nil
				}
			}}
			state, err := login.StartBrowser(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			u, _ := url.Parse(state.URL)
			if u.Host != "linux.do" || u.Query().Get("scopes") != "one_time_password" || state.Status != "callback" {
				t.Fatal("incorrect browser authorization")
			}
			block, _ := pem.Decode([]byte(u.Query().Get("public_key")))
			key, err := x509.ParsePKIXPublicKey(block.Bytes)
			if err != nil {
				t.Fatal(err)
			}
			encrypt := func(raw []byte) string {
				cipher, err := rsa.EncryptPKCS1v15(rand.Reader, key.(*rsa.PublicKey), raw)
				if err != nil {
					t.Fatal(err)
				}
				return base64.StdEncoding.EncodeToString(cipher)
			}
			nonce := u.Query().Get("nonce")
			if scenario == "wrong nonce" {
				nonce = "stale-authorization"
			}
			payload, _ := json.Marshal(map[string]string{"key": strings.Repeat("a", 32), "nonce": nonce})
			callback := "discourse://auth_redirect?" + url.Values{"payload": {encrypt(payload)}, "oneTimePassword": {encrypt([]byte(strings.Repeat("c", 32)))}}.Encode()
			done, err := login.CompleteBrowser(context.Background(), state.ID, callback)
			if scenario == "success" {
				if err != nil || done.Status != "success" || saves != 1 || calls != 3 {
					t.Fatal(done, err, saves, calls)
				}
				if _, err := login.CompleteBrowser(context.Background(), state.ID, callback); err == nil {
					t.Fatal("consumed login result replayed")
				}
			} else if err == nil || saves != 0 || (scenario == "wrong nonce" && calls != 0) || (scenario == "blocked" && calls != 1) {
				t.Fatal("invalid authorization saved or retried")
			}
		})
	}
}

func TestBrowserCallbackURLAcceptsDiscourseJSONResponse(t *testing.T) {
	callback := "discourse://auth_redirect?payload=payload-value&oneTimePassword=otp-value"
	for _, raw := range []string{callback, `{"redirect_url":"` + callback + `"}`} {
		got, err := browserCallbackURL(raw)
		if err != nil || got != callback {
			t.Fatalf("browserCallbackURL(%q) = %q, %v", raw, got, err)
		}
	}
}

func TestBrowserCallbackURLRejectsIncompleteResults(t *testing.T) {
	for _, raw := range []string{"", `{"redirect_url":""}`, "https://example.com/callback", "discourse://auth_redirect?payload=only", "https://linux.do/user-api-key/new?nonce=fixture"} {
		if _, err := browserCallbackURL(raw); err == nil {
			t.Fatalf("browserCallbackURL(%q) accepted incomplete callback", raw)
		}
	}
}
