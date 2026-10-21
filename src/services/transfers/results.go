package transfers

import "github.com/owiks/golang-minio-s3-backend-api/src/services/folders"

type TransferItems struct {
	Items []folders.FolderDeleteItem `json:"items"`
}
