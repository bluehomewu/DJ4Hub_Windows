package main

import (
	"crypto/subtle"
	"net"
	"net/http"
)

// controlShutdown lets `dj4ghub stop` end the background service gracefully.
// It requires the per-launch token that only the launcher knows.
func (a *app) controlShutdown(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(host).IsLoopback() || a.controlToken == "" ||
		subtle.ConstantTimeCompare([]byte(r.Header.Get(controlTokenHeader)), []byte(a.controlToken)) != 1 {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"stopping": true})
	a.shutdownOnce.Do(func() {
		if a.shutdownRequested != nil {
			close(a.shutdownRequested)
		}
	})
}
