package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

func Authenticate(state *runtime.State, r *http.Request, id string) (*runtime.Session, error) {
	access, connection, err := state.LookupAccess(r.Context(), id)
	authorization := r.Header.Values("Authorization")
	if err != nil {
		if !authstate.IsMissing(err) {
			return nil, err
		}
		if len(authorization) > 0 {
			return nil, invalidBearer()
		}
		return nil, apperror.New(401, "SessionExpired", messages.HttpSessionExpired)
	}
	d := access.Snapshot()
	if !connection.Snapshot().Active || !access.Valid(time.Now()) {
		if len(authorization) > 0 || d.Mode == "bearer" {
			return nil, invalidBearer()
		}
		return nil, apperror.New(401, "SessionExpired", messages.HttpSessionExpired)
	}
	previous := false
	if len(authorization) > 0 || d.Mode == "bearer" {
		if d.Mode != "bearer" || len(authorization) != 1 {
			return nil, invalidBearer()
		}
		scheme, token, ok := strings.Cut(authorization[0], " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") {
			return nil, invalidBearer()
		}
		matched, old := access.Match(strings.TrimLeft(token, " "), time.Now())
		if !matched {
			return nil, invalidBearer()
		}
		previous = old
	} else {
		cookie, err := r.Cookie("minio_session_" + id)
		if err != nil || !authstate.Match(cookie.Value, d.Digest) {
			return nil, apperror.New(401, "SessionExpired", messages.HttpSessionExpiredSession)
		}
		if r.Method != http.MethodGet && (!common.SameOrigin(r, state.Config().Origin) || !authstate.Match(r.Header.Get("X-CSRF-Token"), d.CSRFDigest)) {
			return nil, apperror.New(403, "CSRFDenied", messages.HttpCSRFDenied)
		}
	}
	session, err := state.BindAccess(r.Context(), access, connection, previous)
	if err != nil {
		return nil, err
	}
	if time.Since(d.LastUsedAt) >= time.Minute {
		if err = state.Auth.Touch(r.Context(), id, time.Now().UTC()); err != nil {
			return nil, err
		}
	}
	return session, nil
}

func invalidBearer() error {
	return apperror.New(401, "InvalidToken", messages.HttpInvalidBearerToken)
}
