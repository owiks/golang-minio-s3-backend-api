package folders

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	objectssvc "minio-web/api/src/services/objects"
	"minio-web/api/src/services/objects/state"
	"minio-web/api/src/services/result"
)

func InspectFolder(ctx context.Context, s FolderDeleteItemsClient, bucket, prefix string) (FolderDeletionInfo, error) {
	bucket, prefix, err := FolderDeleteParams(bucket, prefix)
	if err != nil {
		return FolderDeletionInfo{}, err
	}
	items, err := FolderDeleteItems(ctx, s, bucket, prefix)
	if err != nil {
		return FolderDeletionInfo{}, err
	}
	var size int64
	files, markers := 0, 0
	for _, item := range items {
		size += item.Size
		if item.Marker {
			markers++
		} else {
			files++
		}
	}
	return FolderDeletionInfo{Items: items, Files: files, Markers: markers, Size: size}, nil
}

func InspectFolderObject(ctx context.Context, s InspectFolderObjectClient, bucket, prefix string, key string) (FolderObjectInfo, error) {
	bucket, prefix, err := FolderDeleteParams(bucket, prefix)
	if err != nil {
		return FolderObjectInfo{}, err
	}

	if key == "" || len(key) > 1024 || !utf8.ValidString(key) || !strings.HasPrefix(key, prefix) {
		return FolderObjectInfo{}, apperror.New(400, "InvalidFolderDelete", "Объект не относится к выбранной папке.")
	}

	snapshot, err := objectssvc.ReadDeletion(ctx, s, bucket, key)
	if err != nil {
		return FolderObjectInfo{}, err
	}
	return FolderObjectInfo{Revision: objectstate.DeletionRevision(snapshot), Size: snapshot.Size}, nil
}

func DeleteFolderObject(ctx context.Context, s DeleteFolderObjectClient, bucket, prefix string, key string, body FolderDeleteRequest) (result.Success, error) {
	bucket, prefix, err := FolderDeleteParams(bucket, prefix)
	if err != nil {
		return result.Success{}, err
	}

	if key == "" || len(key) > 1024 || !utf8.ValidString(key) || !strings.HasPrefix(key, prefix) {
		return result.Success{}, apperror.New(400, "InvalidFolderDelete", "Объект не относится к выбранной папке.")
	}

	if body.Confirmation != prefix {
		return result.Success{}, apperror.New(400, "ConfirmationRequired", "Подтвердите точный путь удаляемой папки.")
	}
	if body.Revision == "" {
		return result.Success{}, apperror.New(400, "RevisionRequired", "Сначала проверьте объект перед удалением.")
	}

	snapshot, err := objectssvc.ReadDeletion(ctx, s, bucket, key)
	if err != nil {
		return result.Success{}, err
	}

	if body.Revision != objectstate.DeletionRevision(snapshot) {
		return result.Success{}, apperror.New(409, "ObjectChanged", "Объект изменился после проверки и не удалён. Перечитайте содержимое папки перед повтором.")
	}
	if snapshot.Size == 0 && strings.HasSuffix(key, "/") {
		children, err := FolderMarkerHasChildren(ctx, s, bucket, key)
		if err != nil {
			return result.Success{}, err
		}
		if children {
			return result.Success{}, apperror.New(409, "FolderNotEmpty", "Запись папки сохранена: внутри остались объекты. Проверьте ошибки или перечитайте папку.")
		}
	}
	// Match ordinary file deletion: never pass VersionID, ForceDelete or GovernanceBypass.
	if err := s.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{}); err != nil {
		switch minio.ToErrorResponse(err).Code {
		case "AccessDenied", "InvalidRequest", "ObjectLocked":
			return result.Success{}, apperror.New(403, "ObjectDeleteDenied", "MinIO запретил удаление. Проверьте права, политику бакета и защиту объекта.")
		}
		return result.Success{}, err
	}
	return result.Success{OK: true}, nil
}

type FolderDeleteRequest struct {
	Revision     string `json:"revision"`
	Confirmation string `json:"confirmation"`
}
