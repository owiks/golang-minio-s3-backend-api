package controllers

import (
	"net/http"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/validation"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/sessions"
)

type RevisionRequest struct {
	Revision int64 `json:"revision" validate:"required"`
}
type CredentialsRequest struct {
	Revision     int64  `json:"revision" validate:"required"`
	AccessKey    string `json:"accessKey" validate:"required,maxBytes=256"`
	SecretKey    string `json:"secretKey" validate:"required,maxBytes=4096"`
	SessionToken string `json:"sessionToken,omitempty"`
}

type TokenIssueRequest struct {
	Name       string           `json:"name"`
	TTLSeconds *int64           `json:"ttlSeconds" validate:"required"`
	Policy     authstate.Policy `json:"policy"`
}

type TokenRenewRequest struct {
	Revision   int64  `json:"revision" validate:"required"`
	TTLSeconds *int64 `json:"ttlSeconds" validate:"required"`
}

// CurrentView extends the caller's access projection with safe operation and TTL
// limits. It never exposes operator credentials or private connection settings.
type CurrentView struct {
	authstate.AccessView
	EffectiveOperations EffectiveOperationsView `json:"effectiveOperations"`
	MaxTokenTTLSeconds  int64                   `json:"maxTokenTTLSeconds"`
}

type EffectiveOperationsView struct {
	ReadOnly            bool `json:"readOnly"`
	DisableUploads      bool `json:"disableUploads"`
	DisableDeletes      bool `json:"disableDeletes"`
	DisableAdminChanges bool `json:"disableAdminChanges"`
}

func Current(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, _ string) error {
	policy := app.Config().Operations
	common.WriteJSON(w, r, 200, CurrentView{AccessView: s.Access().View(), EffectiveOperations: EffectiveOperationsView{
		ReadOnly: policy.ReadOnly, DisableUploads: policy.DisableUploads,
		DisableDeletes: policy.DisableDeletes, DisableAdminChanges: policy.DisableAdminChanges,
	}, MaxTokenTTLSeconds: int64(app.Config().TokenTTL / time.Second)})
	return nil
}
func Connections(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, _ string) error {
	m, g := managerFor(app, s), s.AccessGuard()
	id := r.PathValue("connectionID")
	if id == "" {
		v, err := m.Connections(r.Context(), g)
		if err != nil {
			return err
		}
		common.WriteJSON(w, r, 200, v)
		return nil
	}
	if r.Method == "GET" {
		v, err := m.Connection(r.Context(), g, id)
		if err != nil {
			return err
		}
		common.WriteJSON(w, r, 200, v.View())
		return nil
	}
	var req RevisionRequest
	if err := validation.Decode(w, r, &req); err != nil {
		return err
	}
	if err := m.DeleteConnection(r.Context(), g, id, req.Revision); err != nil {
		return err
	}
	common.WriteJSON(w, r, 200, common.Success{OK: true})
	return nil
}
func Credentials(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, _ string) error {
	var req CredentialsRequest
	if err := validation.Decode(w, r, &req); err != nil {
		return err
	}
	m, g, id := managerFor(app, s), s.AccessGuard(), r.PathValue("connectionID")
	release, _, err := app.AdmitLogin("credential-management")
	if err != nil {
		return err
	}
	defer release()
	if err = m.ReplaceCredentials(r.Context(), g, id, req.Revision, authstate.Secrets{AccessKey: req.AccessKey, SecretKey: req.SecretKey, SessionToken: req.SessionToken}, sessions.NewCredentialVerifier(app.Config(), app)); err != nil {
		return err
	}
	common.WriteJSON(w, r, 200, common.Success{OK: true})
	return nil
}
func Tokens(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, _ string) error {
	m, g, id := managerFor(app, s), s.AccessGuard(), r.PathValue("connectionID")
	if r.Method == "GET" {
		v, err := m.Accesses(r.Context(), g, id)
		if err != nil {
			return err
		}
		common.WriteJSON(w, r, 200, v)
		return nil
	}
	var req TokenIssueRequest
	if err := validation.Decode(w, r, &req); err != nil {
		return err
	}
	if req.TTLSeconds == nil {
		return authstate.Invalid()
	}
	v, err := m.Issue(r.Context(), g, id, authstate.IssueRequest{Name: req.Name, TTLSeconds: *req.TTLSeconds, Policy: req.Policy})
	if err != nil {
		return err
	}
	common.WriteJSON(w, r, 201, v)
	return nil
}
func Edit(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, _ string) error {
	var req authstate.EditRequest
	if err := validation.Decode(w, r, &req); err != nil {
		return err
	}
	v, err := managerFor(app, s).Edit(r.Context(), s.AccessGuard(), r.PathValue("tokenID"), req)
	if err != nil {
		return err
	}
	common.WriteJSON(w, r, 200, v)
	return nil
}
func Revoke(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, _ string) error {
	var req RevisionRequest
	if err := validation.Decode(w, r, &req); err != nil {
		return err
	}
	if err := managerFor(app, s).Revoke(r.Context(), s.AccessGuard(), r.PathValue("tokenID"), req.Revision); err != nil {
		return err
	}
	common.WriteJSON(w, r, 200, common.Success{OK: true})
	return nil
}
func Rotate(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, _ string) error {
	var req authstate.RotateRequest
	if err := validation.Decode(w, r, &req); err != nil {
		return err
	}
	v, err := managerFor(app, s).Rotate(r.Context(), s.AccessGuard(), r.PathValue("tokenID"), req)
	if err != nil {
		return err
	}
	common.WriteJSON(w, r, 200, v)
	return nil
}
func Renew(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, _ string) error {
	var req TokenRenewRequest
	if err := validation.Decode(w, r, &req); err != nil {
		return err
	}
	if req.TTLSeconds == nil {
		return authstate.Invalid()
	}
	v, err := managerFor(app, s).Renew(r.Context(), s.AccessGuard(), r.PathValue("tokenID"), authstate.RenewRequest{Revision: req.Revision, TTLSeconds: *req.TTLSeconds})
	if err != nil {
		return err
	}
	common.WriteJSON(w, r, 200, v)
	return nil
}
func Events(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, _ string) error {
	v, err := managerFor(app, s).Events(r.Context(), s.AccessGuard())
	if err != nil {
		return err
	}
	common.WriteJSON(w, r, 200, v)
	return nil
}

func GetToken(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, _ string) error {
	v, err := managerFor(app, s).Token(r.Context(), s.AccessGuard(), r.PathValue("tokenID"))
	if err != nil {
		return err
	}
	common.WriteJSON(w, r, 200, v)
	return nil
}

func managerFor(app *runtime.State, s *runtime.Session) *authstate.Manager {
	return s.AccessManager().WithVerifier(sessions.NewCredentialVerifier(app.Config(), app))
}
