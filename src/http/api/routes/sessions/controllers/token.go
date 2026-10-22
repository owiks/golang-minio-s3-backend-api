package controllers

import (
	"net/http"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/validation"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/sessions"
)

func HandleToken(app *runtime.State, w http.ResponseWriter, r *http.Request) error {
	// CLI clients have no Origin. A browser-supplied Origin must still match.
	origins := r.Header.Values("Origin")
	if len(origins) > 0 && (len(origins) != 1 || !common.SameOrigin(r, app.Config().Origin)) {
		return apperror.New(403, "OriginDenied", messages.HttpOriginDenied)
	}
	release, err := admitLogin(app, w, r)
	if err != nil {
		return err
	}
	defer release()
	var request sessions.TokenRequest
	if err := validation.Decode(w, r, &request); err != nil {
		return err
	}
	value, err := sessions.IssueToken(r.Context(), app.Config(), app, request)
	if err != nil {
		if apperror.Public(err).Code == "LoginRateLimit" {
			w.Header().Set("Retry-After", "3")
		}
		return err
	}
	common.WriteJSON(w, r, http.StatusCreated, value)
	return nil
}
