package server

import "net/http"

func (b *Backend) route(w http.ResponseWriter, r *http.Request) {
	if b.routeCapabilities(w, r) {
		return
	}
	if b.routePlatformLogin(w, r) {
		return
	}
	if b.routeModelDirectory(w, r) {
		return
	}
	if b.routeDownloadDirectory(w, r) {
		return
	}
	if b.routePathSelection(w, r) {
		return
	}
	b.handler.ServeHTTP(w, r)
}
