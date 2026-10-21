package uploads

import (
	"context"
	"strings"
	"time"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/result"
	"minio-web/api/src/services/securetoken"
	"minio-web/api/src/services/uploads/invite"
	validationsvc "minio-web/api/src/services/validation"
)

func ListUploadInvites(ctx context.Context, store UploadInviteLister, owner string, bucket, prefix string) ([]uploadinvite.UploadInviteSummary, error) {
	return store.List(owner, bucket, prefix), nil
}

func RevokeUploadInvite(ctx context.Context, store UploadInviteRevoker, owner string, token string) (result.Success, error) {
	if err := store.Revoke(owner, token); err != nil {
		return result.Success{}, err
	}
	return result.Success{OK: true}, nil
}

func CreateUploadInvite(ctx context.Context, store UploadInvitePublisher, s CreateUploadInviteClient, body UploadInvitesRequest) (uploadinvite.View, error) {
	if body.Bucket == "" || len(body.Bucket) > 63 || len(body.Prefix) > 900 || body.Prefix != "" && !strings.HasSuffix(body.Prefix, "/") || !validationsvc.ValidFileKey(body.Prefix+"invite-file") || body.ExpiresIn < 60 || body.ExpiresIn > 86400 || body.MaxSize < 1 || body.MaxSize > MaxRemoteSize || body.MaxFiles < 1 || body.MaxFiles > 100 {
		return uploadinvite.View{}, apperror.New(400, "InvalidInvite", "Проверьте папку, срок (до суток), размер (до 1 ГиБ) и количество файлов (до 100).")
	}
	// UI permission hints are not authoritative; verify the native scope before
	// delegating it. Every recipient PUT is additionally authorized by S3 itself.
	allowed, err := EvaluateInviteScope(ctx, s, body.Bucket, body.Prefix)
	if err != nil {
		return uploadinvite.View{}, err
	}
	if !allowed {
		return uploadinvite.View{}, apperror.New(403, "InviteDenied", "Нет подтверждённого права записи во всю выбранную папку.")
	}
	g := uploadinvite.New(uploadinvite.Config{Owner: s.SessionID(), Token: securetoken.New(), Bucket: body.Bucket, Prefix: body.Prefix, Expires: time.Now().Add(time.Duration(body.ExpiresIn) * time.Second), MaxSize: body.MaxSize, MaxFiles: body.MaxFiles, Writer: s})
	if err := store.Add(g); err != nil {
		return uploadinvite.View{}, err
	}
	return g.Snapshot(), nil
}

type UploadInvitesRequest struct {
	Bucket    string `json:"bucket"`
	Prefix    string `json:"prefix"`
	ExpiresIn int    `json:"expiresIn"`
	MaxSize   int64  `json:"maxSize"`
	MaxFiles  int    `json:"maxFiles"`
}
