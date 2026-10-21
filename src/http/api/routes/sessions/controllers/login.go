package controllers

import (
	"math"
	"net/http"
	"strconv"

	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/middleware"
	httpvalidation "github.com/owiks/golang-minio-s3-backend-api/src/http/validation"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	sessionssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/sessions"
)

func HandleLogin(app *runtimesvc.State, w http.ResponseWriter, r *http.Request) error {
	cfg := app.Config()
	if !httpcommon.SameOrigin(r, cfg.Origin) {
		return apperror.New(403, "OriginDenied", messages.HttpOriginDenied)
	}
	release, retryAfter, err := app.AdmitLogin(middleware.ClientAddress(r, cfg.TrustedProxies))
	if err != nil {
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(retryAfter.Seconds())))))
		return err
	}
	defer release()
	var c runtimesvc.Connection
	if err := httpvalidation.Decode(w, r, &c); err != nil {
		return err
	}
	s, value, err := sessionssvc.Connect(r.Context(), cfg, app, c)
	if err != nil {
		if apperror.Public(err).Code == "LoginRateLimit" {
			w.Header().Set("Retry-After", "3")
		}
		return err
	}
	http.SetCookie(w, httpcommon.SessionCookie(s, r, false))
	httpcommon.WriteJSON(w, r, 201, value)
	return nil
}
