package server

import (
	"crypto/rand"
	"crypto/subtle"
	"net"
	"net/http"
	"time"
)

// StartLoopback serves the desktop app on a random 127.0.0.1 port. A
// per-launch token, exchanged once for a cookie, keeps other local processes
// out. A managed loopback origin gives embedded players a browser Referer and
// lets WebKit use its native HTTP media loading and cache behavior.
func (b *Backend) StartLoopback() (string, func(), error) {
	if b.runtime == nil {
		return "", nil, ErrRuntimeUnavailable
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	token := rand.Text()
	host := listener.Addr().String()
	origin := "http://" + host
	bootstrap := "/desktop-session/" + token
	server := &http.Server{ReadHeaderTimeout: 10 * time.Second, IdleTimeout: time.Minute}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestOrigin := r.Header.Get("Origin")
		if r.Host != host || (requestOrigin != "" && requestOrigin != origin) || !sameOrigin(r) {
			http.Error(w, "forbidden origin", 403)
			return
		}
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		if r.Method == "GET" && r.URL.Path == bootstrap {
			http.SetCookie(w, &http.Cookie{Name: "tangentry-desktop", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			w.Header().Set("Cache-Control", "no-store")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		cookie, err := r.Cookie("tangentry-desktop")
		if err != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(token)) != 1 {
			http.Error(w, "forbidden session", 403)
			return
		}
		forward(b, w, r)
	})
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		_ = server.Close()
		return "", nil, ErrBackendClosed
	}
	b.loopbackMu.Lock()
	if b.loopback != nil {
		b.loopbackMu.Unlock()
		b.mu.Unlock()
		_ = server.Close()
		return "", nil, ErrLoopbackBusy
	}
	b.loopback = server
	b.loopbackMu.Unlock()
	b.mu.Unlock()
	go func() { _ = server.Serve(listener) }()
	return origin + bootstrap, func() { b.closeLoopback() }, nil
}
