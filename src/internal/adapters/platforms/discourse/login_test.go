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
	"strings"
	"testing"
)

func TestDeviceAuthorizationChecksNonceAndHidesPrivateData(t *testing.T) {
	for _, tampered := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "wrong nonce"}[tampered], func(t *testing.T) {
			var public *rsa.PublicKey
			var nonce, client string
			saves := 0
			login := &Login{PrepareSave: func() func(string, string, int64) error {
				return func(key, id string, expires int64) error {
					saves++
					if key != strings.Repeat("a", 32) || id != client {
						t.Fatal("bad saved credentials")
					}
					return nil
				}
			}, HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "linux.do" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
					t.Fatal("login leaks existing credentials")
				}
				var body map[string]string
				if json.NewDecoder(r.Body).Decode(&body) != nil {
					t.Fatal("invalid body")
				}
				if r.URL.Path == "/user-api-key/device.json" {
					if body["scopes"] != "read" {
						t.Fatal("excessive permissions")
					}
					nonce, client = body["nonce"], body["client_id"]
					block, _ := pem.Decode([]byte(body["public_key"]))
					parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
					if err != nil {
						t.Fatal(err)
					}
					public = parsed.(*rsa.PublicKey)
					return response(r, 200, `{"device_code":"`+strings.Repeat("f", 64)+`","user_code":"ABCD1234","verification_uri_with_request":"https://linux.do/user-api-key/activate?request=12345678","expires_in":600,"interval":5}`), nil
				}
				if body["device_code"] != strings.Repeat("f", 64) || body["client_id"] != client {
					t.Fatal("bad poll")
				}
				if tampered {
					nonce = "another-attempt"
				}
				plain, _ := json.Marshal(map[string]string{"key": strings.Repeat("a", 32), "nonce": nonce})
				cipher, err := rsa.EncryptPKCS1v15(rand.Reader, public, plain)
				if err != nil {
					t.Fatal(err)
				}
				raw, _ := json.Marshal(map[string]string{"status": "authorized", "payload": base64.StdEncoding.EncodeToString(cipher)})
				return response(r, 200, string(raw)), nil
			})}}
			state, err := login.Start(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(state)
			if strings.Contains(string(raw), "private") || strings.Contains(string(raw), strings.Repeat("f", 64)) {
				t.Fatal("private state exposed")
			}
			done, err := login.Poll(context.Background(), state.ID)
			if tampered {
				if err == nil || saves != 0 {
					t.Fatal(done, err, saves)
				}
			} else {
				if err != nil || done.Status != "success" || saves != 1 {
					t.Fatal(done, err, saves)
				}
			}
			login.Cancel(state.ID)
			if _, err := login.Poll(context.Background(), state.ID); err == nil {
				t.Fatal("canceled attempt retained")
			}
		})
	}
}

func TestDeviceAuthorizationRejectsForeignConfirmationPage(t *testing.T) {
	login := &Login{PrepareSave: func() func(string, string, int64) error {
		return func(string, string, int64) error { t.Fatal("unexpected save"); return nil }
	}, HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		return response(r, 200, `{"device_code":"`+strings.Repeat("a", 64)+`","user_code":"ABCD1234","verification_uri":"https://evil.example/confirm","expires_in":600,"interval":5}`), nil
	})}}
	if _, err := login.Start(context.Background()); err == nil {
		t.Fatal("foreign confirmation page accepted")
	}
}
