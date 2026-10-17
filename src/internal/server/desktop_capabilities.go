package server

import "net/http"

func (b *Backend) routeCapabilities(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/capabilities" || r.Method != http.MethodGet {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	desktopJSON(w, http.StatusOK, b.Capabilities())
	return true
}
