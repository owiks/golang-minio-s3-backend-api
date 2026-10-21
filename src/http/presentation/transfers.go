package presentation

import (
	"minio-web/api/src/services/localization"
	"minio-web/api/src/services/transfers"
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
