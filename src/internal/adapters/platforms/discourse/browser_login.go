package discourse

import (
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
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"tangentry/internal/discovery"
)

// A web app cannot claim the discourse:// scheme. The owner explicitly submits
// the encrypted callback; only this process can decrypt its one-time token.
func (l *Login) StartBrowser(ctx context.Context) (LoginState, error) {
	if l.HTTP == nil || l.PrepareCookieSave == nil {
		return LoginState{}, discovery.ErrUnavailable
	}
	l.mu.Lock()
	if l.sessions == nil {
		l.sessions = map[string]*loginAttempt{}
	}
	for id, attempt := range l.sessions {
		if attempt.state.ExpiresAt <= time.Now().Unix() {
			delete(l.sessions, id)
		}
	}
	if len(l.sessions)+l.starting >= 4 {
		l.mu.Unlock()
		return LoginState{}, discovery.ErrUnavailable
	}
	l.starting++
	l.mu.Unlock()
	defer func() { l.mu.Lock(); l.starting--; l.mu.Unlock() }()
	save := l.PrepareCookieSave()
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil || ctx.Err() != nil {
		return LoginState{}, discovery.ErrUnavailable
	}
	public, err := x509.MarshalPKIXPublicKey(&private.PublicKey)
	if err != nil {
		return LoginState{}, discovery.ErrUnavailable
	}
	id, nonce, client := randomID(), randomID(), randomID()
	if id == "" || nonce == "" || client == "" {
		return LoginState{}, discovery.ErrUnavailable
	}
	query := url.Values{
		"application_name": {"Tangentry"}, "client_id": {client}, "nonce": {nonce},
		"scopes": {"one_time_password"}, "auth_redirect": {"discourse://auth_redirect"},
		"public_key": {string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public}))},
		"padding":    {"pkcs1"}, "expires_in_seconds": {"3600"},
	}
	state := LoginState{ID: id, URL: "https://linux.do/user-api-key/new?" + query.Encode(), Status: "callback", ExpiresAt: time.Now().Add(10 * time.Minute).Unix()}
	l.mu.Lock()
	l.sessions[id] = &loginAttempt{state: state, private: private, nonce: nonce, client: client, cookieSave: save}
	l.mu.Unlock()
	return state, nil
}

func (l *Login) CompleteBrowser(ctx context.Context, id, callback string) (LoginState, error) {
	callback, err := browserCallbackURL(callback)
	if err != nil {
		return LoginState{}, err
	}
	l.mu.Lock()
	attempt := l.sessions[id]
	if attempt == nil || attempt.state.Status != "callback" || attempt.pending || attempt.state.ExpiresAt <= time.Now().Unix() {
		l.mu.Unlock()
		return LoginState{}, discovery.ErrInvalidQuery
	}
	attempt.pending = true
	snapshot := *attempt
	l.mu.Unlock()
	cookies, err := l.redeemCallback(ctx, &snapshot, callback)
	l.mu.Lock()
	defer l.mu.Unlock()
	attempt.pending = false
	if l.sessions[id] != attempt || ctx.Err() != nil {
		return LoginState{}, discovery.ErrInvalidQuery
	}
	attempt.private = nil
	attempt.nonce = ""
	attempt.state.URL = ""
	if err != nil {
		attempt.state.Status = "blocked"
		attempt.cookieSave = nil
		return LoginState{}, err
	}
	if err := attempt.cookieSave(cookies); err != nil {
		attempt.state.Status = "changed"
		attempt.cookieSave = nil
		return LoginState{}, &discovery.PlatformError{Message: "登录配置已更换，请重新授权。"}
	}
	attempt.cookieSave = nil
	attempt.state.Status = "success"
	return attempt.state, nil
}

// Discourse returns redirect_url in the authorization request's JSON response.
// Accept that response as well as the URI so the owner need not edit secrets.
func browserCallbackURL(raw string) (string, error) {
	invalid := &discovery.PlatformError{Message: "授权结果不完整，请复制原站响应中的 redirect_url，或改用 Cookie。"}
	if len(raw) > 8192 {
		return "", invalid
	}
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "https://linux.do/user-api-key/new") {
		return "", &discovery.PlatformError{Message: "这是授权发起链接，不是登录结果。请在授权页点击“创建一次性登录令牌”，再复制跳转后的 discourse:// 回调；也可以改用 Cookie。"}
	}
	if strings.HasPrefix(raw, "{") {
		var response struct {
			URL string `json:"redirect_url"`
		}
		if json.Unmarshal([]byte(raw), &response) != nil {
			return "", invalid
		}
		raw = response.URL
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "discourse" || u.Host != "auth_redirect" || u.User != nil || u.Path != "" || u.Fragment != "" {
		return "", invalid
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(query["payload"]) != 1 || query.Get("payload") == "" || len(query["oneTimePassword"]) != 1 || query.Get("oneTimePassword") == "" {
		return "", invalid
	}
	return raw, nil
}

func (l *Login) redeemCallback(ctx context.Context, attempt *loginAttempt, callback string) ([]*http.Cookie, error) {
	u, err := url.Parse(callback)
	if err != nil || len(callback) > 8192 || u.Scheme != "discourse" || u.Host != "auth_redirect" || u.User != nil || u.Path != "" || u.Fragment != "" {
		return nil, discovery.ErrInvalidQuery
	}
	decrypt := func(value string) ([]byte, error) {
		cipher, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return nil, discovery.ErrInvalidQuery
		}
		plain, err := rsa.DecryptPKCS1v15(rand.Reader, attempt.private, cipher)
		if err != nil {
			return nil, discovery.ErrInvalidQuery
		}
		return plain, nil
	}
	payload, err := decrypt(u.Query().Get("payload"))
	if err != nil {
		return nil, err
	}
	var value struct {
		Key   string `json:"key"`
		Nonce string `json:"nonce"`
	}
	if json.Unmarshal(payload, &value) != nil || value.Nonce != attempt.nonce || len(value.Key) != 32 {
		return nil, discovery.ErrInvalidQuery
	}
	if _, err := hex.DecodeString(value.Key); err != nil {
		return nil, discovery.ErrInvalidQuery
	}
	plain, err := decrypt(u.Query().Get("oneTimePassword"))
	if err != nil || len(plain) < 16 || len(plain) > 256 {
		return nil, discovery.ErrInvalidQuery
	}
	otp := string(plain)
	if _, err := hex.DecodeString(otp); err != nil || strings.ToLower(otp) != otp {
		return nil, discovery.ErrInvalidQuery
	}
	client := *l.HTTP
	client.Jar, _ = cookiejar.New(nil)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://linux.do/session/csrf.json", nil)
	req.Header.Set("Accept", "application/json")
	setDiscourseBrowserHeaders(req, true)
	res, err := client.Do(req)
	if err != nil {
		return nil, discovery.ErrUnavailable
	}
	defer res.Body.Close()
	if err := browserResponseError(res.StatusCode); err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, (32<<10)+1))
	var csrf struct {
		Token string `json:"csrf"`
	}
	if err != nil || len(raw) > 32<<10 || json.Unmarshal(raw, &csrf) != nil || csrf.Token == "" || len(csrf.Token) > 1024 || strings.ContainsAny(csrf.Token, "\r\n") {
		return nil, discovery.ErrUnavailable
	}
	req, _ = http.NewRequestWithContext(ctx, http.MethodPost, "https://linux.do/session/otp/"+otp, nil)
	req.Header.Set("X-CSRF-Token", csrf.Token)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	setDiscourseBrowserHeaders(req, true)
	req.Header.Set("Origin", "https://linux.do")
	req.Header.Set("Referer", "https://linux.do/user-api-key/new")
	res, err = client.Do(req)
	if err != nil {
		return nil, discovery.ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusFound {
		if err := browserResponseError(res.StatusCode); err != nil {
			return nil, err
		}
		return nil, &discovery.PlatformError{Message: "授权结果已过期或未能登录，请重新授权。"}
	}
	target, err := req.URL.Parse(res.Header.Get("Location"))
	if err != nil || target.Scheme != "https" || target.Host != "linux.do" || target.User != nil {
		return nil, discovery.ErrUnavailable
	}
	loginCookie := false
	for _, cookie := range res.Cookies() {
		if cookie.Name == "_t" && cookie.Value != "" && cookie.Secure && cookie.MaxAge >= 0 && (cookie.Domain == "" || strings.TrimPrefix(cookie.Domain, ".") == "linux.do") {
			loginCookie = true
		}
	}
	if !loginCookie {
		return nil, discovery.ErrUnavailable
	}
	session, err := (&Client{HTTP: &client}).CurrentSession(ctx)
	if err != nil {
		return nil, err
	}
	if !session.LoggedIn {
		return nil, &discovery.PlatformError{Message: "授权后未能确认账号，请重新登录原站。"}
	}
	origin, _ := url.Parse("https://linux.do/")
	cookies := client.Jar.Cookies(origin)
	for _, cookie := range cookies {
		cookie.Secure = true
		cookie.Path = "/"
	}
	return cookies, nil
}

func browserResponseError(status int) error {
	switch status {
	case 200:
		return nil
	case 403:
		return &discovery.PlatformError{Message: "Linux.do 拒绝登录请求，请在原站完成验证，或手动导入 Cookie。", Restricted: true}
	case 429:
		return &discovery.PlatformError{Message: "Linux.do 暂时限制请求，请稍后手动重试。", Restricted: true}
	default:
		return discovery.ErrUnavailable
	}
}
