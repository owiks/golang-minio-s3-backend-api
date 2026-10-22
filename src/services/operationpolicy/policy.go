// Package operationpolicy applies operator maintenance restrictions independently
// of a caller's permissions. The zero policy preserves ordinary API operation.
package operationpolicy

import (
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

// Action describes side effects, not the HTTP verb used to request them.
type Action uint8

const (
	Write Action = 1 << iota
	Upload
	Delete
	Admin
)

// Policy is a process configuration snapshot. Negative switches keep the zero
// value permissive, including API callers constructing a partial Config.
// Cancellation, cleanup and access revocation are deliberately not mutations
// of user data and remain available during maintenance.
type Policy struct {
	ReadOnly            bool `env:"API_READ_ONLY"`
	DisableUploads      bool `env:"API_DISABLE_UPLOADS"`
	DisableDeletes      bool `env:"API_DISABLE_DELETES"`
	DisableAdminChanges bool `env:"API_DISABLE_ADMIN_CHANGES"`
}

func (p Policy) Check(action Action) error {
	if p.ReadOnly && action != 0 || p.DisableUploads && action&Upload != 0 || p.DisableDeletes && action&Delete != 0 || p.DisableAdminChanges && action&Admin != 0 {
		return apperror.New(503, "OperationDisabled", messages.OperationDisabled)
	}
	return nil
}
