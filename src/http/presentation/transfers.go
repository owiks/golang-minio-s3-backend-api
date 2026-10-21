package presentation

import (
	"github.com/owiks/golang-minio-s3-backend-api/src/services/localization"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/transfers"
)

type TransferResult struct {
	transfers.TransferResult
	Message string `json:"message"`
}

func presentTransfersTransferResult(localizer *localization.Localizer, v transfers.TransferResult) TransferResult {
	return TransferResult{TransferResult: v,
		Message: localizer.Render(v.Message),
	}
}
