package xiaoyuzhou

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
)

// AuthSession refreshes Xiaoyuzhou app credentials at most once for a
// concurrent batch of requests. The credential callbacks keep secrets out of
// the platform adapter and allow the store to enforce compare-and-swap rules.
type AuthSession struct {
	HTTP   *http.Client
	Tokens func() (access, refresh string)
	Save   func(oldAccess, oldRefresh, access, refresh string) error
	mu     sync.Mutex
	busy   bool
	wait   chan struct{}
}

func (a *AuthSession) Do(ctx context.Context, req *http.Request, access string) (*http.Response, error) {
	client := *a.client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	var retryBody []byte
	if req.Body != nil && req.GetBody != nil {
		if body, err := req.GetBody(); err == nil {
			retryBody, _ = io.ReadAll(body)
			body.Close()
		}
	}
	res, err := client.Do(req)
	if err != nil || res.StatusCode != http.StatusUnauthorized || a.Tokens == nil || a.Save == nil {
		return res, err
	}
	res.Body.Close()
	if err := a.refresh(ctx, access); err != nil {
		return nil, err
	}
	if fresh, _ := a.Tokens(); fresh != "" {
		retry := req.Clone(ctx)
		retry.Header.Set("x-jike-access-token", fresh)
		if retryBody != nil {
			retry.Body = io.NopCloser(bytes.NewReader(retryBody))
			retry.ContentLength = int64(len(retryBody))
		}
		return client.Do(retry)
	}
	return nil, errRefreshUnavailable
}

func (a *AuthSession) client() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return http.DefaultClient
}

var errRefreshUnavailable = &refreshError{"xiaoyuzhou token refresh unavailable"}

type refreshError struct{ message string }

func (e *refreshError) Error() string { return e.message }

func (a *AuthSession) refresh(ctx context.Context, failedAccess string) error {
	a.mu.Lock()
	if a.busy {
		wait := a.wait
		a.mu.Unlock()
		select {
		case <-wait:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	a.busy, a.wait = true, make(chan struct{})
	wait := a.wait
	a.mu.Unlock()
	defer func() { a.mu.Lock(); a.busy = false; close(wait); a.mu.Unlock() }()
	access, refresh := a.Tokens()
	if access == "" || refresh == "" || (failedAccess != "" && access != failedAccess) {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.xiaoyuzhoufm.com/app_auth_tokens.refresh", nil)
	if err != nil {
		return errRefreshUnavailable
	}
	setAppHeaders(req, access)
	req.Header.Set("x-jike-refresh-token", refresh)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	client := *a.client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return errRefreshUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 256<<10))
	if err != nil {
		return errRefreshUnavailable
	}
	newAccess, newRefresh := res.Header.Get("x-jike-access-token"), res.Header.Get("x-jike-refresh-token")
	var payload struct {
		AccessToken   string `json:"accessToken"`
		RefreshToken  string `json:"refreshToken"`
		HeaderAccess  string `json:"x-jike-access-token"`
		HeaderRefresh string `json:"x-jike-refresh-token"`
	}
	_ = json.Unmarshal(raw, &payload)
	if newAccess == "" {
		newAccess = payload.AccessToken
	}
	if newRefresh == "" {
		newRefresh = payload.RefreshToken
	}
	if newAccess == "" {
		newAccess = payload.HeaderAccess
	}
	if newRefresh == "" {
		newRefresh = payload.HeaderRefresh
	}
	if newAccess == "" || newRefresh == "" {
		return errRefreshUnavailable
	}
	return a.Save(access, refresh, newAccess, newRefresh)
}
