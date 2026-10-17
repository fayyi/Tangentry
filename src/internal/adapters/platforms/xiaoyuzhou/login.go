package xiaoyuzhou

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"tangentry/internal/discovery"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

var ErrLogin = errors.New("xiaoyuzhou QR login unavailable")

type LoginState struct {
	ID        string `json:"id"`
	Image     string `json:"image,omitempty"`
	Status    string `json:"status"`
	ExpiresAt int64  `json:"expiresAt"`
}
type loginSession struct {
	upstream string
	state    LoginState
	next     time.Time
	busy     bool
	save     func(string, string) error
}
type Login struct {
	HTTP        *http.Client
	Save        func(string, string) error
	PrepareSave func() func(string, string) error
	mu          sync.Mutex
	sessions    map[string]*loginSession
}

func (l *Login) request(ctx context.Context, path string, payload any) (*http.Response, []byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, ErrLogin
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://web-api.xiaoyuzhoufm.com/v1/auth/qrcode/"+path, bytes.NewReader(body))
	if err != nil {
		return nil, nil, ErrLogin
	}
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("x-midway-app-id", "v6worU4NnWyL")
	req.Header.Set("Origin", "https://accounts.xiaoyuzhoufm.com/")
	req.Header.Set("Referer", "https://accounts.xiaoyuzhoufm.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 Tangentry/0.1")
	client := *l.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		return nil, nil, ErrLogin
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, (64<<10)+1))
	if err != nil || res.StatusCode != http.StatusOK || len(raw) > 64<<10 {
		return nil, nil, ErrLogin
	}
	return res, raw, nil
}
func randomID() (string, error) {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", ErrLogin
	}
	return hex.EncodeToString(raw[:]), nil
}

func (l *Login) Start(ctx context.Context) (LoginState, error) {
	if l.HTTP == nil || l.Save == nil {
		return LoginState{}, ErrLogin
	}
	save := l.Save
	if l.PrepareSave != nil {
		save = l.PrepareSave()
	}
	_, raw, err := l.request(ctx, "create", map[string]string{"clientId": "xyz-web"})
	if err != nil {
		return LoginState{}, err
	}
	var value struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if json.Unmarshal(raw, &value) != nil || len(value.ID) < 16 || len(value.ID) > 128 {
		return LoginState{}, ErrLogin
	}
	u, err := url.Parse(value.URL)
	if err != nil || u.Scheme != "https" || u.Host != "h5.xiaoyuzhoufm.com" || u.User != nil || u.Path != "/oauth" || u.Query().Get("qrcode_id") != value.ID {
		return LoginState{}, ErrLogin
	}
	png, err := qrcode.Encode(value.URL, qrcode.Medium, 224)
	if err != nil {
		return LoginState{}, ErrLogin
	}
	id, err := randomID()
	if err != nil {
		return LoginState{}, err
	}
	state := LoginState{ID: id, Image: "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), Status: "waiting", ExpiresAt: time.Now().Add(180 * time.Second).Unix()}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.sessions == nil {
		l.sessions = map[string]*loginSession{}
	}
	now := time.Now().Unix()
	for key, session := range l.sessions {
		if session.state.ExpiresAt <= now {
			delete(l.sessions, key)
		}
	}
	if len(l.sessions) >= 4 {
		return LoginState{}, ErrLogin
	}
	l.sessions[id] = &loginSession{upstream: value.ID, state: state, save: save}
	return state, nil
}

func (l *Login) Poll(ctx context.Context, id string) (LoginState, error) {
	l.mu.Lock()
	session := l.sessions[id]
	if session == nil {
		l.mu.Unlock()
		return LoginState{}, discovery.ErrUnavailable
	}
	if session.state.Status == "success" || session.state.Status == "expired" {
		state := session.state
		l.mu.Unlock()
		return state, nil
	}
	if session.state.ExpiresAt <= time.Now().Unix() {
		session.state.Status = "expired"
		state := session.state
		l.mu.Unlock()
		return state, nil
	}
	if session.busy || time.Now().Before(session.next) {
		state := session.state
		l.mu.Unlock()
		return state, nil
	}
	session.next = time.Now().Add(time.Second)
	session.busy = true
	upstream := session.upstream
	l.mu.Unlock()
	defer func() { l.mu.Lock(); session.busy = false; l.mu.Unlock() }()
	res, raw, err := l.request(ctx, "login", map[string]string{"id": upstream})
	if err != nil {
		return LoginState{}, err
	}
	var value struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return LoginState{}, ErrLogin
	}
	status := strings.ToUpper(value.Status)
	if status != "WAITTING" && status != "SCANNED" && status != "USED" && status != "CONFIRMED" && status != "EXPIRED" {
		return LoginState{}, ErrLogin
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	session = l.sessions[id]
	if session == nil {
		return LoginState{}, discovery.ErrUnavailable
	}
	if session.state.Status == "success" || session.state.Status == "expired" {
		return session.state, nil
	}
	if session.state.ExpiresAt <= time.Now().Unix() {
		session.state.Status, session.state.Image = "expired", ""
		return session.state, nil
	}
	switch status {
	case "EXPIRED":
		session.state.Status = "expired"
	case "SCANNED":
		session.state.Status = "scanned"
	case "USED", "CONFIRMED":
		access, refresh := res.Header.Get("x-jike-access-token"), res.Header.Get("x-jike-refresh-token")
		if access == "" || refresh == "" {
			return LoginState{}, ErrLogin
		}
		if err := session.save(access, refresh); err != nil {
			return LoginState{}, err
		}
		session.state.Status, session.state.Image = "success", ""
	}
	return session.state, nil
}
