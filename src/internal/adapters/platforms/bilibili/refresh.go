package bilibili

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// CredentialRefresher performs Bilibili's browser-cookie rotation protocol.
// The callbacks keep storage policy outside the platform adapter.
type CredentialRefresher struct {
	HTTP  *http.Client
	Load  func() (cookie, refresh string)
	Save  func(oldCookie, oldRefresh, newCookie, newRefresh string) error
	mu    sync.Mutex
	check time.Time
}

var refreshCSRF = regexp.MustCompile(`<div id="1-name">([^<]+)</div>`)

func (r *CredentialRefresher) Ensure(ctx context.Context) error {
	if r == nil || r.Load == nil || r.Save == nil {
		return nil
	}
	r.mu.Lock()
	if time.Now().Before(r.check) {
		r.mu.Unlock()
		return nil
	}
	r.check = time.Now().Add(10 * time.Minute)
	r.mu.Unlock()
	cookie, refresh := r.Load()
	if cookie == "" || refresh == "" {
		return nil
	}
	if !r.needsRefresh(ctx, cookie) {
		return nil
	}
	newCookie, newRefresh, _, err := r.rotate(ctx, cookie, refresh)
	if err != nil {
		return err
	}
	if err = r.Save(cookie, refresh, newCookie, newRefresh); err != nil {
		return err
	}
	// Persist first; confirmation invalidates the old refresh token. A retrying
	// process can still use the newly persisted credential if this call fails.
	return r.confirm(ctx, newCookie, refresh)
}

func (r *CredentialRefresher) client() *http.Client {
	if r.HTTP != nil {
		return r.HTTP
	}
	return http.DefaultClient
}

func (r *CredentialRefresher) request(ctx context.Context, method, rawURL, cookie string, body io.Reader, headers http.Header) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	client := *r.client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return client.Do(req)
}

func (r *CredentialRefresher) needsRefresh(ctx context.Context, cookie string) bool {
	res, err := r.request(ctx, http.MethodGet, "https://passport.bilibili.com/x/passport-login/web/cookie/info", cookie, nil, nil)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return false
	}
	var value struct {
		Data struct {
			Refresh bool `json:"refresh"`
		} `json:"data"`
	}
	return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&value) == nil && value.Data.Refresh
}

func (r *CredentialRefresher) rotate(ctx context.Context, cookie, refresh string) (string, string, string, error) {
	path, err := correspondPath()
	if err != nil {
		return "", "", "", err
	}
	res, err := r.request(ctx, http.MethodGet, "https://www.bilibili.com/correspond/1/"+path, cookie, nil, nil)
	if err != nil {
		return "", "", "", err
	}
	raw, readErr := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	res.Body.Close()
	if readErr != nil || res.StatusCode != http.StatusOK {
		return "", "", "", errors.New("bilibili refresh csrf unavailable")
	}
	match := refreshCSRF.FindSubmatch(raw)
	if len(match) != 2 {
		return "", "", "", errors.New("bilibili refresh csrf missing")
	}
	form := url.Values{"csrf": {cookieValue(cookie, "bili_jct")}, "refresh_csrf": {string(match[1])}, "refresh_token": {refresh}, "source": {"main_web"}}
	headers := http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}
	res, err = r.request(ctx, http.MethodPost, "https://passport.bilibili.com/x/passport-login/web/cookie/refresh", cookie, strings.NewReader(form.Encode()), headers)
	if err != nil {
		return "", "", "", err
	}
	defer res.Body.Close()
	raw, err = io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil || res.StatusCode != http.StatusOK {
		return "", "", "", errors.New("bilibili cookie refresh failed")
	}
	var value struct {
		Data struct {
			Refresh string `json:"refresh_token"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &value) != nil || value.Data.Refresh == "" {
		return "", "", "", errors.New("bilibili refresh token missing")
	}
	newCookie := mergeCookies(cookie, joinCookies(res.Cookies()))
	if cookieValue(newCookie, "SESSDATA") == "" || cookieValue(newCookie, "bili_jct") == "" {
		return "", "", "", errors.New("bilibili rotated cookies missing")
	}
	return newCookie, value.Data.Refresh, string(match[1]), nil
}

func (r *CredentialRefresher) confirm(ctx context.Context, cookie, oldRefresh string) error {
	form := url.Values{"csrf": {cookieValue(cookie, "bili_jct")}, "refresh_token": {oldRefresh}}
	res, err := r.request(ctx, http.MethodPost, "https://passport.bilibili.com/x/passport-login/web/confirm/refresh", cookie, strings.NewReader(form.Encode()), http.Header{"Content-Type": {"application/x-www-form-urlencoded"}})
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return errors.New("bilibili refresh confirmation failed")
	}
	return nil
}

func correspondPath() (string, error) {
	block, _ := pem.Decode([]byte(`-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDLgd2OAkcGVtoE3ThUREbio0Eg
Uc/prcajMKXvkCKFCWhJYJcLkcM2DKKcSeFpD/j6Boy538YXnR6VhcuUJOhH2x71
nzPjfdTcqMz7djHum0qSZA0AyCBDABUqCrfNgCiJ00Ra7GmRj+YCK1NJEuewlb40
JNrRuoEUXpabUzGB8QIDAQAB
-----END PUBLIC KEY-----
`))
	if block == nil {
		return "", errors.New("bilibili refresh key missing")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", err
	}
	rsaKey, ok := key.(*rsa.PublicKey)
	if !ok {
		return "", errors.New("bilibili refresh key invalid")
	}
	stamp := time.Now().Add(-20 * time.Second).UnixMilli()
	ciphertext, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, rsaKey, []byte(fmt.Sprintf("refresh_%d", stamp)), nil)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(ciphertext), nil
}

func cookieValue(header, name string) string {
	for _, pair := range strings.Split(header, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if ok && key == name {
			return value
		}
	}
	return ""
}

func joinCookies(cookies []*http.Cookie) string {
	values := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie.Name != "" && cookie.Value != "" {
			values = append(values, cookie.Name+"="+cookie.Value)
		}
	}
	return strings.Join(values, "; ")
}

func mergeCookies(previous, updated string) string {
	order := []string{}
	values := map[string]string{}
	for _, header := range []string{previous, updated} {
		for _, pair := range strings.Split(header, ";") {
			name, value, ok := strings.Cut(strings.TrimSpace(pair), "=")
			if !ok || name == "" || value == "" {
				continue
			}
			if _, exists := values[name]; !exists {
				order = append(order, name)
			}
			values[name] = value
		}
	}
	parts := make([]string, 0, len(order))
	for _, name := range order {
		parts = append(parts, name+"="+values[name])
	}
	return strings.Join(parts, "; ")
}
