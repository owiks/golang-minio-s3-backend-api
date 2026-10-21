package uploads

import (
	"context"
	"strings"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/telemetry"
)

type IncompleteUploadOptions struct {
	Bucket, Prefix, Key, UploadID, KeyMarker, UploadIDMarker string
	Summary                                                  bool
}

func InspectIncompleteUpload(ctx context.Context, s InspectIncompleteUploadClient, input IncompleteUploadOptions) (IncompleteUploadState, error) {
	bucket, key, id := input.Bucket, input.Key, input.UploadID
	if bucket == "" || len(bucket) > 63 || key == "" || len(key) > 1024 || id == "" || len(id) > 4096 {
		return IncompleteUploadState{}, apperror.New(400, "InvalidUploadListing", "Выберите загрузку для проверки частей.")
	}
	parts, err := InspectParts(ctx, s, bucket, key, id)
	if minio.ToErrorResponse(err).Code == "NoSuchUpload" {
		return IncompleteUploadState{State: "absent"}, nil
	}
	if err != nil {
		return IncompleteUploadState{}, err
	}
	if input.Summary {
		parts.Items = nil
	}
	return IncompleteUploadState{State: "present", Parts: &parts}, nil
}

func AbortIncompleteUpload(ctx context.Context, s AbortIncompleteUploadClient, body IncompleteUploadsRequest) (AbortUploadResult, error) {
	if body.Bucket == "" || len(body.Bucket) > 63 || body.Key == "" || len(body.Key) > 1024 || body.UploadID == "" || len(body.UploadID) > 4096 || body.Confirmation != "ОТМЕНИТЬ" {
		return AbortUploadResult{}, apperror.New(400, "InvalidAbort", "Выберите загрузку и введите ОТМЕНИТЬ для подтверждения.")
	}
	current, err := InspectParts(ctx, s, body.Bucket, body.Key, body.UploadID)
	if minio.ToErrorResponse(err).Code == "NoSuchUpload" {
		return AbortUploadResult{State: "absent"}, nil
	}
	if err != nil {
		return AbortUploadResult{}, err
	}
	if current.Revision != body.Revision || body.Revision == "" {
		return AbortUploadResult{}, apperror.New(409, "UploadChanged", "Список частей изменился. Возможно, загрузка продолжается. Обновите список перед отменой.")
	}
	if err := s.AbortMultipartUpload(ctx, body.Bucket, body.Key, body.UploadID); err != nil {
		return AbortUploadResult{}, err
	}
	// Abort can race with an in-flight part. Never report confirmed cancellation
	// until ListParts says the exact upload no longer exists.
	_, checkErr := InspectParts(ctx, s, body.Bucket, body.Key, body.UploadID)
	state, message := "uncertain", "Не удалось подтвердить отмену. Проверьте результат перед повтором."
	if minio.ToErrorResponse(checkErr).Code == "NoSuchUpload" {
		state = "cancelled"
		message = "Отмена подтверждена. Готовый объект не удалялся."
	} else if checkErr == nil {
		state = "remaining"
		message = "Загрузка ещё доступна. Проверьте части перед повторной отменой."
	}
	if state != "cancelled" {
		telemetry.Outcome(ctx, "partial", "AbortUnconfirmed")
	}
	return AbortUploadResult{OK: boolPointer(state == "cancelled"), State: state, Message: message}, nil
}

func ListIncompleteUploads(ctx context.Context, s ListIncompleteUploadsClient, input IncompleteUploadOptions) (IncompleteUploadsPage, error) {
	bucket, prefix := input.Bucket, input.Prefix
	if bucket == "" || len(bucket) > 63 || len(prefix) > 1024 || len(input.KeyMarker) > 2048 || len(input.UploadIDMarker) > 8192 {
		return IncompleteUploadsPage{}, apperror.New(400, "InvalidUploadListing", "Выберите бакет и проверьте начало пути.")
	}
	// Some MinIO releases omit matching multipart uploads when a prefix is
	// sent upstream. Summary mode filters a bounded scan locally and returns
	// the exact upstream cursors, including for an empty matching page.
	upstreamPrefix := prefix
	if input.Summary {
		upstreamPrefix = ""
	}
	core := s
	page, err := core.ListMultipartUploads(ctx, bucket, upstreamPrefix, input.KeyMarker, input.UploadIDMarker, "", 25)
	if err != nil {
		return IncompleteUploadsPage{}, err
	}
	if input.Summary {
		for scanned := 0; ; scanned++ {
			matches := page.Uploads[:0]
			for _, upload := range page.Uploads {
				if strings.HasPrefix(upload.Key, prefix) {
					matches = append(matches, upload)
				}
			}
			page.Uploads = matches
			if len(matches) > 0 || !page.IsTruncated || scanned >= 7 {
				break
			}
			previousKey, previousID := page.NextKeyMarker, page.NextUploadIDMarker
			page, err = core.ListMultipartUploads(ctx, bucket, "", previousKey, previousID, "", 25)
			if err != nil {
				return IncompleteUploadsPage{}, err
			}
			if page.IsTruncated && page.NextKeyMarker == previousKey && page.NextUploadIDMarker == previousID {
				return IncompleteUploadsPage{}, apperror.New(502, "InvalidUploadCursor", "MinIO не обновил позицию списка. Повторите запрос.")
			}
		}
	}
	rows := make([]IncompleteUploadRow, 0, len(page.Uploads))
	for _, upload := range page.Uploads {
		item := IncompleteUploadRow{Key: upload.Key, ID: upload.UploadID, Initiated: upload.Initiated, Owner: upload.Owner.DisplayName}
		if item.Owner == "" {
			item.Owner = upload.Owner.ID
		}
		item.Initiator = upload.Initiator.DisplayName
		if item.Initiator == "" {
			item.Initiator = upload.Initiator.ID
		}
		item.StorageClass = upload.StorageClass
		if input.Summary {
			rows = append(rows, item)
			continue
		}
		parts, err := InspectParts(ctx, s, bucket, upload.Key, upload.UploadID)
		if err != nil {
			item.Error = apperror.Public(err).Message
		} else {
			item.Parts = &parts
		}
		rows = append(rows, item)
	}
	return IncompleteUploadsPage{Items: rows, HasMore: page.IsTruncated, KeyMarker: page.NextKeyMarker, UploadIDMarker: page.NextUploadIDMarker}, nil
}

type IncompleteUploadsRequest struct {
	Bucket       string `json:"bucket"`
	Key          string `json:"key"`
	UploadID     string `json:"uploadId"`
	Revision     string `json:"revision"`
	Confirmation string `json:"confirmation"`
}

func boolPointer(value bool) *bool { return &value }
