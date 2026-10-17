package bilibili

import (
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
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

var ErrLogin = errors.New("bilibili QR login unavailable")

type LoginState struct {
	ID        string `json:"id"`
	Image     string `json:"image,omitempty"`
	Status    string `json:"status"`
	ExpiresAt int64  `json:"expiresAt"`
}
type loginSession struct {
	key   string
	state LoginState
	next  time.Time
	save  func(string, string) error
}
type QRLogin struct {
	mu          sync.Mutex
	HTTP        *http.Client
	Save        func(string) error
	PrepareSave func() func(string, string) error
	sessions    map[string]*loginSession
}

func (l *QRLogin) request(ctx context.Context, path string) (*http.Response, []byte, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://passport.bilibili.com"+path, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 Tangentry/0.1")
	client := *l.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		return nil, nil, ErrLogin
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, (64<<10)+1))
	if err != nil || res.StatusCode != 200 || len(raw) > 64<<10 {
		return nil, nil, ErrLogin
	}
	return res, raw, nil
}
func (l *QRLogin) Start(ctx context.Context) (LoginState, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.sessions == nil {
		l.sessions = map[string]*loginSession{}
	}
	for id, s := range l.sessions {
		if s.state.ExpiresAt <= time.Now().Unix() {
			delete(l.sessions, id)
		}
	}
	if len(l.sessions) >= 4 {
		return LoginState{}, ErrLogin
	}
	var save func(string, string) error
	if l.PrepareSave != nil {
		save = l.PrepareSave()
	}
	_, raw, err := l.request(ctx, "/x/passport-login/web/qrcode/generate")
	if err != nil {
		return LoginState{}, err
	}
	var data struct {
		Code int `json:"code"`
		Data struct {
			URL string `json:"url"`
			Key string `json:"qrcode_key"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &data) != nil || data.Code != 0 || len(data.Data.Key) < 16 || len(data.Data.Key) > 256 {
		return LoginState{}, ErrLogin
	}
	u, err := url.Parse(data.Data.URL)
	if err != nil || u.Scheme != "https" || u.Host != "account.bilibili.com" || u.User != nil || u.Query().Get("qrcode_key") != data.Data.Key {
		return LoginState{}, ErrLogin
	}
	png, err := qrcode.Encode(data.Data.URL, qrcode.Medium, 224)
	if err != nil {
		return LoginState{}, ErrLogin
	}
	var id [24]byte
	if _, err = rand.Read(id[:]); err != nil {
		return LoginState{}, ErrLogin
	}
	state := LoginState{ID: hex.EncodeToString(id[:]), Image: "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), Status: "waiting", ExpiresAt: time.Now().Add(180 * time.Second).Unix()}
	l.sessions[state.ID] = &loginSession{key: data.Data.Key, state: state, save: save}
	return state, nil
}
func (l *QRLogin) Poll(ctx context.Context, id string) (LoginState, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.sessions[id]
	if s == nil {
		return LoginState{}, ErrLogin
	}
	if s.state.ExpiresAt <= time.Now().Unix() {
		s.state.Status = "expired"
	}
	if s.state.Status == "expired" || s.state.Status == "success" || time.Now().Before(s.next) {
		return s.state, nil
	}
	s.next = time.Now().Add(2 * time.Second)
	res, raw, err := l.request(ctx, "/x/passport-login/web/qrcode/poll?qrcode_key="+url.QueryEscape(s.key))
	if err != nil {
		return LoginState{}, err
	}
	var data struct {
		Code int `json:"code"`
		Data struct {
			Code    int    `json:"code"`
			Refresh string `json:"refresh_token"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &data) != nil || data.Code != 0 {
		return LoginState{}, ErrLogin
	}
	switch data.Data.Code {
	case 86101:
		s.state.Status = "waiting"
	case 86090:
		s.state.Status = "scanned"
	case 86038:
		s.state.Status = "expired"
	case 0:
		pairs := []string{}
		hasSession, hasCSRF := false, false
		for _, cookie := range res.Cookies() {
			domain := strings.TrimPrefix(strings.ToLower(cookie.Domain), ".")
			if domain != "bilibili.com" && domain != "passport.bilibili.com" {
				continue
			}
			if cookie.Name == "SESSDATA" && cookie.Value != "" {
				hasSession = true
			}
			if cookie.Name == "bili_jct" && cookie.Value != "" {
				hasCSRF = true
			}
			pairs = append(pairs, cookie.Name+"="+cookie.Value)
		}
		if !hasSession || !hasCSRF || s.save == nil && l.Save == nil {
			return LoginState{}, ErrLogin
		}
		if s.save != nil {
			err = s.save(strings.Join(pairs, "; "), data.Data.Refresh)
		} else {
			err = l.Save(strings.Join(pairs, "; "))
		}
		if err != nil {
			return LoginState{}, err
		}
		s.state.Status = "success"
		s.key = ""
		s.save = nil
		s.state.Image = ""
	default:
		return LoginState{}, ErrLogin
	}
	return s.state, nil
}
