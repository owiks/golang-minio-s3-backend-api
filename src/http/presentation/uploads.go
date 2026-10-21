package presentation

import (
	"minio-web/api/src/services/localization"
	"minio-web/api/src/services/uploads"
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
