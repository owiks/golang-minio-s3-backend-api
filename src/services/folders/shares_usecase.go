package folders

import (
	"context"
	"time"

	"minio-web/api/src/services/apperror"
	foldershare "minio-web/api/src/services/folders/share"
	"minio-web/api/src/services/iam"
	"minio-web/api/src/services/messages"
	"minio-web/api/src/services/result"
	"minio-web/api/src/services/securetoken"
)

func ListFolderShares(ctx context.Context, store FolderShareLister, owner string, bucket, prefix string) ([]foldershare.FolderShareSummary, error) {
	return store.List(owner, bucket, prefix), nil
}

func RevokeFolderShare(ctx context.Context, store FolderShareRevoker, owner string, token string) (result.Success, error) {
	if err := store.Revoke(owner, token); err != nil {
		return result.Success{}, err
	}
	return result.Success{OK: true}, nil
}

func CreateFolderShare(ctx context.Context, store FolderSharePublisher, s CreateFolderShareClient, body FolderSharesRequest) (foldershare.View, error) {
	if body.Bucket == "" || len(body.Bucket) > 63 || !SharePrefix(body.Prefix) || body.ExpiresIn < 60 || body.ExpiresIn > 86400 {
		return foldershare.View{}, apperror.New(400, "InvalidFolderShare", messages.FoldersInvalidFolderShare)
	}
	if issue := s.RefreshActionPolicy(ctx); issue != nil {
		return foldershare.View{}, apperror.New(403, "SharePermissionsUnavailable", messages.FoldersSharePermissionsUnavailable)
	}
	policy := s.ActionPolicySnapshot()
	allowed := policy.Allows(s.ActorName(), iam.Query{Action: "s3:ListBucket", Bucket: body.Bucket}) && policy.Allows(s.ActorName(), iam.Query{Action: "s3:GetObject", Bucket: body.Bucket, Key: body.Prefix + "*"})
	if !allowed {
		return foldershare.View{}, apperror.New(403, "FolderShareDenied", messages.FoldersFolderShareDenied)
	}
	g := foldershare.New(foldershare.Config{Owner: s.SessionID(), Token: securetoken.New(), Bucket: body.Bucket, Prefix: body.Prefix, Expires: time.Now().Add(time.Duration(body.ExpiresIn) * time.Second), Reader: s})
	if err := store.Add(g); err != nil {
		return foldershare.View{}, err
	}
	return g.View(), nil
}

type FolderSharesRequest struct {
	Bucket    string `json:"bucket"`
	Prefix    string `json:"prefix"`
	ExpiresIn int    `json:"expiresIn"`
}
