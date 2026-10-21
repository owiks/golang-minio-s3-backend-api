package common

import "net/http"

func SameOrigin(r *http.Request, configured string) bool {
	origin := r.Header.Get("Origin")
	if configured != "" {
		return origin == configured
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return origin == scheme+"://"+r.Host
}
