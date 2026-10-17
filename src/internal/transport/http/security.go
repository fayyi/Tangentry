package httptransport

import (
	"net/http"
	"net/url"
	"strings"
)

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
			if !validHost(r.Host) || !validOrigin(r) {
				write(w, http.StatusForbidden, apiError{"forbidden", "不允许此来源访问。"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func validHost(host string) bool {
	u, err := url.Parse("http://" + host)
	return err == nil && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")
}

func validOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return r.Header.Get("Sec-Fetch-Site") != "cross-site"
	}
	if origin == "http://"+r.Host {
		return true
	}
	return origin == "http://127.0.0.1:5173" || origin == "http://localhost:5173"
}
