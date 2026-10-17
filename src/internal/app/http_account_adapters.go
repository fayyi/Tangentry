package app

import (
	"context"

	"tangentry/internal/adapters/platforms/bilibili"
	"tangentry/internal/adapters/platforms/discourse"
	"tangentry/internal/adapters/platforms/xiaoyuzhou"
	httptransport "tangentry/internal/transport/http"
)

type bilibiliLogin struct{ client *bilibili.QRLogin }
type xiaoyuzhouLogin struct{ client *xiaoyuzhou.Login }
type linuxdoLogin struct{ client *discourse.Login }
type linuxdoSession struct{ client *discourse.Client }

func newHTTPAccountAdapters(p *platforms, network platformNetwork) httptransport.AccountDependencies {
	cookies := network.cookies
	return httptransport.AccountDependencies{
		BilibiliLogin:   bilibiliLogin{&bilibili.QRLogin{HTTP: network.public, PrepareSave: cookies.PrepareBilibiliLogin}},
		XiaoyuzhouLogin: xiaoyuzhouLogin{&xiaoyuzhou.Login{HTTP: network.public, Save: cookies.SaveTokens, PrepareSave: cookies.PrepareTokenLogin}},
		LinuxdoLogin:    linuxdoLogin{&discourse.Login{HTTP: network.public, PrepareSave: cookies.PrepareLinuxdoAuthorization, PrepareCookieSave: cookies.PrepareLinuxdoCookieLogin}},
		LinuxdoSession:  linuxdoSession{p.discovery.forum}.Current,
		Cookies:         cookies,
	}
}

func qrLoginState(id, image, status string, expiresAt int64) httptransport.QRLoginState {
	return httptransport.QRLoginState{ID: id, Image: image, Status: status, ExpiresAt: expiresAt}
}

func (l bilibiliLogin) Start(ctx context.Context) (httptransport.QRLoginState, error) {
	state, err := l.client.Start(ctx)
	return qrLoginState(state.ID, state.Image, state.Status, state.ExpiresAt), err
}

func (l bilibiliLogin) Poll(ctx context.Context, id string) (httptransport.QRLoginState, error) {
	state, err := l.client.Poll(ctx, id)
	return qrLoginState(state.ID, state.Image, state.Status, state.ExpiresAt), err
}

func (l xiaoyuzhouLogin) Start(ctx context.Context) (httptransport.QRLoginState, error) {
	state, err := l.client.Start(ctx)
	return qrLoginState(state.ID, state.Image, state.Status, state.ExpiresAt), err
}

func (l xiaoyuzhouLogin) Poll(ctx context.Context, id string) (httptransport.QRLoginState, error) {
	state, err := l.client.Poll(ctx, id)
	return qrLoginState(state.ID, state.Image, state.Status, state.ExpiresAt), err
}

func linuxdoState(state discourse.LoginState) httptransport.LinuxdoState {
	return httptransport.LinuxdoState{ID: state.ID, URL: state.URL, Code: state.Code, Status: state.Status, ExpiresAt: state.ExpiresAt, Interval: state.Interval}
}

func (l linuxdoLogin) Start(ctx context.Context) (httptransport.LinuxdoState, error) {
	state, err := l.client.Start(ctx)
	return linuxdoState(state), err
}

func (l linuxdoLogin) StartBrowser(ctx context.Context) (httptransport.LinuxdoState, error) {
	state, err := l.client.StartBrowser(ctx)
	return linuxdoState(state), err
}

func (l linuxdoLogin) Poll(ctx context.Context, id string) (httptransport.LinuxdoState, error) {
	state, err := l.client.Poll(ctx, id)
	return linuxdoState(state), err
}

func (l linuxdoLogin) CompleteBrowser(ctx context.Context, id, callback string) (httptransport.LinuxdoState, error) {
	state, err := l.client.CompleteBrowser(ctx, id, callback)
	return linuxdoState(state), err
}

func (l linuxdoLogin) Cancel(id string) { l.client.Cancel(id) }

func (s linuxdoSession) Current(ctx context.Context) (httptransport.LinuxdoSession, error) {
	state, err := s.client.CurrentSession(ctx)
	return httptransport.LinuxdoSession{LoggedIn: state.LoggedIn, Username: state.Username}, err
}

var _ httptransport.LoginFlow[httptransport.QRLoginState] = bilibiliLogin{}
var _ httptransport.LoginFlow[httptransport.QRLoginState] = xiaoyuzhouLogin{}
var _ httptransport.BrowserLoginFlow[httptransport.LinuxdoState] = linuxdoLogin{}
