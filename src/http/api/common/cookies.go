package common

import (
	"net/http"
	"time"

	"minio-web/api/src/services/runtime"
)

func SessionCookie(s *runtime.Session, r *http.Request, remove bool) *http.Cookie {
	snapshot := s.CookieSnapshot()
	age := int(time.Until(snapshot.Expires).Seconds())
	if remove {
		age = -1
	}
	return &http.Cookie{Name: "minio_session_" + snapshot.ID, Value: snapshot.Token, Path: "/api/sessions/" + snapshot.ID, HttpOnly: true, Secure: snapshot.Secure || r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https", SameSite: http.SameSiteStrictMode, MaxAge: age}
}
