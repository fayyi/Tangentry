package server

import (
	"net/http"
	"net/url"
)

// The application's API accepts only localhost hosts and origins. A gate
// checks the request against the address it was actually served on, then
// forwards it as a same-origin localhost request.
func forward(next http.Handler, w http.ResponseWriter, r *http.Request) {
	cloned := r.Clone(r.Context())
	cloned.URL.Scheme, cloned.URL.Host, cloned.Host = "http", "localhost", "localhost"
	if cloned.Header.Get("Origin") != "" {
		cloned.Header.Set("Origin", "http://localhost")
	}
	next.ServeHTTP(w, cloned)
}

// sameOrigin rejects cross-site requests and any Origin other than the host
// the request was sent to. Scheme is not compared: a TLS-terminating proxy
// forwards https origins over plain HTTP.
func sameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host == r.Host && u.Path == ""
}
