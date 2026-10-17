package discourse

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"tangentry/internal/discovery"
)

type Login struct {
	HTTP              *http.Client
	PrepareSave       func() func(string, string, int64) error
	PrepareCookieSave func() func([]*http.Cookie) error
	mu                sync.Mutex
	sessions          map[string]*loginAttempt
	starting          int
}
type LoginState struct {
	ID        string `json:"id"`
	URL       string `json:"url,omitempty"`
	Code      string `json:"code,omitempty"`
	Status    string `json:"status"`
	ExpiresAt int64  `json:"expiresAt"`
	Interval  int    `json:"interval"`
}
type loginAttempt struct {
	state                 LoginState
	private               *rsa.PrivateKey
	device, nonce, client string
	next                  time.Time
	pending               bool
	save                  func(string, string, int64) error
	cookieSave            func([]*http.Cookie) error
}

func randomID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(value[:])
}

// Device authorization keeps credentials in the Go process. The browser visits
// the site's own confirmation page; it never receives the private key or token.
func (l *Login) Start(ctx context.Context) (LoginState, error) {
	if l.HTTP == nil || l.PrepareSave == nil {
		return LoginState{}, discovery.ErrUnavailable
	}
	l.mu.Lock()
	if l.sessions == nil {
		l.sessions = map[string]*loginAttempt{}
	}
	for key, value := range l.sessions {
		if time.Now().Unix() >= value.state.ExpiresAt {
			delete(l.sessions, key)
		}
	}
	if len(l.sessions)+l.starting >= 4 {
		l.mu.Unlock()
		return LoginState{}, discovery.ErrUnavailable
	}
	l.starting++
	l.mu.Unlock()
	defer func() { l.mu.Lock(); l.starting--; l.mu.Unlock() }()
	save := l.PrepareSave()
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return LoginState{}, discovery.ErrUnavailable
	}
	public, err := x509.MarshalPKIXPublicKey(&private.PublicKey)
	if err != nil {
		return LoginState{}, discovery.ErrUnavailable
	}
	client, nonce, id := randomID(), randomID(), randomID()
	if client == "" || nonce == "" || id == "" {
		return LoginState{}, discovery.ErrUnavailable
	}
	raw, err := l.post(ctx, "/user-api-key/device.json", map[string]string{
		"application_name": "Tangentry", "client_id": client, "nonce": nonce, "scopes": "read",
		"public_key": string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})),
		"padding":    "pkcs1", "expires_in_seconds": "2592000",
	})
	if err != nil {
		return LoginState{}, err
	}
	var response struct {
		Device     string `json:"device_code"`
		Code       string `json:"user_code"`
		URL        string `json:"verification_uri"`
		RequestURL string `json:"verification_uri_with_request"`
		Expires    int    `json:"expires_in"`
		Interval   int    `json:"interval"`
	}
	if json.Unmarshal(raw, &response) != nil || len(response.Device) != 64 || response.Code == "" || response.Expires < 1 || response.Expires > 600 || response.Interval < 1 || response.Interval > 60 {
		return LoginState{}, discovery.ErrUnavailable
	}
	if _, err := hex.DecodeString(response.Device); err != nil {
		return LoginState{}, discovery.ErrUnavailable
	}
	target := response.RequestURL
	if target == "" {
		target = response.URL
	}
	u, err := url.Parse(target)
	if err != nil || u.Scheme != "https" || u.Host != "linux.do" || u.User != nil || u.Path != "/user-api-key/activate" || u.Fragment != "" {
		return LoginState{}, discovery.ErrUnavailable
	}
	state := LoginState{ID: id, URL: target, Code: response.Code, Status: "waiting", ExpiresAt: time.Now().Unix() + int64(response.Expires), Interval: max(response.Interval, 5)}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sessions[id] = &loginAttempt{state: state, private: private, device: response.Device, nonce: nonce, client: client, save: save}
	return state, nil
}

func (l *Login) Cancel(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.sessions, id)
}

func (l *Login) Poll(ctx context.Context, id string) (LoginState, error) {
	l.mu.Lock()
	attempt := l.sessions[id]
	if attempt == nil {
		l.mu.Unlock()
		return LoginState{}, discovery.ErrInvalidQuery
	}
	if time.Now().Unix() >= attempt.state.ExpiresAt {
		attempt.state.Status = "expired"
		attempt.private = nil
	}
	if attempt.state.Status != "waiting" || attempt.pending || time.Now().Before(attempt.next) {
		state := attempt.state
		l.mu.Unlock()
		return state, nil
	}
	attempt.pending = true
	attempt.next = time.Now().Add(time.Duration(attempt.state.Interval) * time.Second)
	l.mu.Unlock()
	raw, err := l.post(ctx, "/user-api-key/device/poll.json", map[string]string{"device_code": attempt.device, "client_id": attempt.client})
	l.mu.Lock()
	defer l.mu.Unlock()
	attempt.pending = false
	if l.sessions[id] != attempt {
		return LoginState{}, discovery.ErrInvalidQuery
	}
	if err != nil {
		attempt.state.Status = "blocked"
		attempt.private = nil
		return LoginState{}, err
	}
	var response struct {
		Status  string `json:"status"`
		Payload string `json:"payload"`
	}
	if json.Unmarshal(raw, &response) != nil {
		return LoginState{}, discovery.ErrUnavailable
	}
	switch response.Status {
	case "authorization_pending":
		return attempt.state, nil
	case "access_denied":
		attempt.state.Status = "denied"
	case "expired_token":
		attempt.state.Status = "expired"
	case "authorized":
		cipher, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(response.Payload), ""))
		if err != nil || attempt.private == nil {
			return LoginState{}, discovery.ErrUnavailable
		}
		plain, err := rsa.DecryptPKCS1v15(rand.Reader, attempt.private, cipher)
		if err != nil {
			return LoginState{}, discovery.ErrUnavailable
		}
		var payload struct {
			Key     string `json:"key"`
			Nonce   string `json:"nonce"`
			Expires string `json:"expires_at"`
		}
		if json.Unmarshal(plain, &payload) != nil || payload.Nonce != attempt.nonce || len(payload.Key) != 32 {
			return LoginState{}, discovery.ErrUnavailable
		}
		if _, err := hex.DecodeString(payload.Key); err != nil {
			return LoginState{}, discovery.ErrUnavailable
		}
		expires := int64(0)
		if payload.Expires != "" {
			date, err := time.Parse(time.RFC3339, payload.Expires)
			if err != nil || !date.After(time.Now()) {
				return LoginState{}, discovery.ErrUnavailable
			}
			expires = date.Unix()
		}
		if err := attempt.save(payload.Key, attempt.client, expires); err != nil {
			attempt.state.Status = "changed"
			attempt.private = nil
			return LoginState{}, &discovery.PlatformError{Message: "登录配置已更换，请重新授权。"}
		}
		attempt.state.Status = "success"
	default:
		return LoginState{}, discovery.ErrUnavailable
	}
	attempt.private = nil
	attempt.device = ""
	attempt.nonce = ""
	attempt.save = nil
	return attempt.state, nil
}

func (l *Login) post(ctx context.Context, path string, value map[string]string) ([]byte, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, discovery.ErrUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://linux.do"+path, bytes.NewReader(body))
	if err != nil {
		return nil, discovery.ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Tangentry/0.1")
	client := *l.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		return nil, discovery.ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode == 400 || res.StatusCode == 403 {
		return nil, &discovery.PlatformError{Message: "Linux.do 未允许本次只读授权，请在原站检查应用授权权限，或使用 Cookie 登录。", Restricted: true}
	}
	if res.StatusCode == 429 {
		return nil, &discovery.PlatformError{Message: "Linux.do 暂时限制登录请求，请稍后手动重试。", Restricted: true}
	}
	if res.StatusCode != 200 {
		return nil, discovery.ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, (32<<10)+1))
	if err != nil || len(raw) > 32<<10 || !json.Valid(raw) {
		return nil, discovery.ErrUnavailable
	}
	return raw, nil
}
