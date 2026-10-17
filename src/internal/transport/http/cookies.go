package httptransport

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"tangentry/internal/credentials"
	"time"
)

func (h *accountsHandler) bilibiliLoginStart(w http.ResponseWriter, r *http.Request) {
	if h.accounts.BilibiliLogin == nil {
		write(w, 503, apiError{"login_unavailable", "扫码登录暂时不可用。"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	value, err := h.accounts.BilibiliLogin.Start(ctx)
	if err != nil {
		write(w, 502, apiError{"login_unavailable", "无法生成B站二维码，请稍后重试。"})
		return
	}
	write(w, 200, value)
}
func (h *accountsHandler) bilibiliLoginPoll(w http.ResponseWriter, r *http.Request) {
	if h.accounts.BilibiliLogin == nil {
		write(w, 503, apiError{"login_unavailable", "扫码登录暂时不可用。"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	value, err := h.accounts.BilibiliLogin.Poll(ctx, r.PathValue("id"))
	if err != nil {
		write(w, 502, apiError{"login_unavailable", "扫码状态暂时无法读取，请重试。"})
		return
	}
	write(w, 200, value)
}

func (h *accountsHandler) xiaoyuzhouLoginStart(w http.ResponseWriter, r *http.Request) {
	if h.accounts.XiaoyuzhouLogin == nil {
		write(w, 503, apiError{"login_unavailable", "小宇宙扫码登录暂时不可用。"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	value, err := h.accounts.XiaoyuzhouLogin.Start(ctx)
	if err != nil {
		write(w, 502, apiError{"login_unavailable", "无法生成小宇宙二维码，请稍后重试。"})
		return
	}
	write(w, 200, value)
}

func (h *accountsHandler) xiaoyuzhouLoginPoll(w http.ResponseWriter, r *http.Request) {
	if h.accounts.XiaoyuzhouLogin == nil {
		write(w, 503, apiError{"login_unavailable", "小宇宙扫码登录暂时不可用。"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	value, err := h.accounts.XiaoyuzhouLogin.Poll(ctx, r.PathValue("id"))
	if err != nil {
		write(w, 502, apiError{"login_unavailable", "小宇宙扫码状态暂时无法读取，请重试。"})
		return
	}
	write(w, 200, value)
}

func (h *accountsHandler) cookieList(w http.ResponseWriter, r *http.Request) {
	if h.accounts.Cookies == nil {
		fail(w, credentials.ErrStorage)
		return
	}
	states := h.accounts.Cookies.States()
	// Username is fetched only as a non-secret status hint. Cookie values and
	// browser User-Agent strings remain inside the credential store.
	linuxConfigured := false
	for _, state := range states {
		if state.Platform == "linuxdo" {
			linuxConfigured = state.Configured
			break
		}
	}
	if linuxConfigured && h.accounts.LinuxdoSession != nil {
		if session, err := h.accounts.LinuxdoSession(r.Context()); err == nil && session.LoggedIn {
			for i := range states {
				if states[i].Platform == "linuxdo" {
					states[i].Username = session.Username
				}
			}
		}
	}
	write(w, 200, map[string]any{"platforms": states})
}
func (h *accountsHandler) cookieSave(w http.ResponseWriter, r *http.Request) {
	if h.accounts.Cookies == nil {
		fail(w, credentials.ErrStorage)
		return
	}
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" {
		write(w, 415, apiError{"invalid_cookie", "请使用 JSON 保存 Cookie。"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 512<<10)
	var value struct {
		Format string `json:"format"`
		Value  string `json:"value"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF {
		fail(w, credentials.ErrInvalid)
		return
	}
	if err := h.accounts.Cookies.Save(r.PathValue("platform"), value.Format, value.Value); err != nil {
		fail(w, err)
		return
	}
	h.cookieList(w, r)
}
func (h *accountsHandler) cookieClear(w http.ResponseWriter, r *http.Request) {
	if h.accounts.Cookies == nil {
		fail(w, credentials.ErrStorage)
		return
	}
	if err := h.accounts.Cookies.Clear(r.PathValue("platform")); err != nil {
		fail(w, err)
		return
	}
	h.cookieList(w, r)
}
