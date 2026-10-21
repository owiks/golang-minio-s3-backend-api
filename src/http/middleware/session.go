package middleware

import (
	"net/http"
	"time"

	"minio-web/api/src/http/api/common"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
	"minio-web/api/src/services/runtime"
)

func Authenticate(state *runtime.State, r *http.Request, id string) (*runtime.Session, error) {
	session := state.Session(id)
	if session == nil || session.Expired(time.Now()) {
		return nil, apperror.New(401, "SessionExpired", messages.HttpSessionExpired)
	}
	cookie, err := r.Cookie("minio_session_" + id)
	if err != nil || !session.MatchesToken(cookie.Value) {
		return nil, apperror.New(401, "SessionExpired", messages.HttpSessionExpiredSession)
	}
	if r.Method != http.MethodGet && (!common.SameOrigin(r, state.Config().Origin) || !session.MatchesCSRF(r.Header.Get("X-CSRF-Token"))) {
		return nil, apperror.New(403, "CSRFDenied", messages.HttpCSRFDenied)
	}
	return session, nil
}
