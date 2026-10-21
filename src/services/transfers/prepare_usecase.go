package transfers

import (
	"context"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	folderssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/folders"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	objectssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/objects"
	objectwrite "github.com/owiks/golang-minio-s3-backend-api/src/services/objects/write"
)

type PrepareFolderOptions struct {
	Bucket, Prefix   string
	VersionRequested bool
}

// PrepareFolderTransfer returns a complete, bounded manifest of the selected
// prefix. A failed listing never becomes a usable partial transfer plan.
func PrepareFolderTransfer(ctx context.Context, s PrepareFolderClient, options PrepareFolderOptions) (TransferItems, error) {
	if options.VersionRequested {
		return TransferItems{}, apperror.New(400, "InvalidFolderDelete", messages.HttpInvalidFolderDelete)
	}
	bucket, prefix, err := folderssvc.FolderDeleteParams(options.Bucket, options.Prefix)
	if err != nil {
		return TransferItems{}, err
	}
	items, err := folderssvc.FolderDeleteItems(ctx, s, bucket, prefix)
	if err != nil {
		if p := apperror.Public(err); p.Code == "FolderDeleteLimit" {
			return TransferItems{}, apperror.New(400, "TransferPlanLimit", messages.FoldersFolderDeleteLimit)
		}
		return TransferItems{}, err
	}
	return TransferItems{Items: items}, nil
}

// PrepareDestination accepts both files and folder markers and preserves the
// opaque S3 key when reading the state used for overwrite confirmation.
func PrepareDestination(ctx context.Context, s objectwrite.StateReader, bucket, key string) (objectwrite.FileState, error) {
	bucket, key, err := objectssvc.ObjectParams(bucket, key)
	if err != nil {
		return objectwrite.FileState{}, err
	}
	if !ValidTransferKey(key) {
		return objectwrite.FileState{}, apperror.New(400, "InvalidTransfer", messages.TransfersInvalidTransferPrepareUsecase)
	}
	return objectwrite.DestinationState(ctx, s, bucket, key)
}
