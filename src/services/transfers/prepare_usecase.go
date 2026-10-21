package transfers

import (
	"context"

	"minio-web/api/src/services/apperror"
	folderssvc "minio-web/api/src/services/folders"
	objectssvc "minio-web/api/src/services/objects"
	"minio-web/api/src/services/objects/write"
)

type PrepareFolderOptions struct {
	Bucket, Prefix   string
	VersionRequested bool
}

// PrepareFolderTransfer returns a complete, bounded manifest of the selected
// prefix. A failed listing never becomes a usable partial transfer plan.
func PrepareFolderTransfer(ctx context.Context, s PrepareFolderClient, options PrepareFolderOptions) (TransferItems, error) {
	if options.VersionRequested {
		return TransferItems{}, apperror.New(400, "InvalidFolderDelete", "Выберите конкретную папку. Удаление всего бакета или отдельных версий этим запросом не выполняется.")
	}
	bucket, prefix, err := folderssvc.FolderDeleteParams(options.Bucket, options.Prefix)
	if err != nil {
		return TransferItems{}, err
	}
	items, err := folderssvc.FolderDeleteItems(ctx, s, bucket, prefix)
	if err != nil {
		if p := apperror.Public(err); p.Code == "FolderDeleteLimit" {
			return TransferItems{}, apperror.New(400, "TransferPlanLimit", "В папке больше 5000 объектов. Выберите меньшую вложенную папку.")
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
		return objectwrite.FileState{}, apperror.New(400, "InvalidTransfer", "Некорректный ключ объекта для переноса.")
	}
	return objectwrite.DestinationState(ctx, s, bucket, key)
}
