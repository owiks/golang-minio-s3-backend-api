package presentation

import (
	"github.com/owiks/golang-minio-s3-backend-api/src/services/localization"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/uploads"
)

type AbortUploadResult struct {
	uploads.AbortUploadResult
	Message string `json:"message,omitempty"`
}

func presentUploadsAbortUploadResult(localizer *localization.Localizer, v uploads.AbortUploadResult) AbortUploadResult {
	return AbortUploadResult{AbortUploadResult: v,
		Message: localizer.Render(v.Message),
	}
}

type IncompleteUploadRow struct {
	uploads.IncompleteUploadRow
	Error string `json:"error,omitempty"`
}

func presentUploadsIncompleteUploadRow(localizer *localization.Localizer, v uploads.IncompleteUploadRow) IncompleteUploadRow {
	return IncompleteUploadRow{IncompleteUploadRow: v,
		Error: localizer.Render(v.Error),
	}
}

type IncompleteUploadsPage struct {
	uploads.IncompleteUploadsPage
	Items []IncompleteUploadRow `json:"items"`
}

func presentUploadsIncompleteUploadsPage(localizer *localization.Localizer, v uploads.IncompleteUploadsPage) IncompleteUploadsPage {
	return IncompleteUploadsPage{IncompleteUploadsPage: v,
		Items: mapSlice(v.Items, func(x uploads.IncompleteUploadRow) IncompleteUploadRow {
			return presentUploadsIncompleteUploadRow(localizer, x)
		}),
	}
}
