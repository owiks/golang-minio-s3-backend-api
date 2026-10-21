package transfers

import "minio-web/api/src/services/folders"

type TransferItems struct {
	Items []folders.FolderDeleteItem `json:"items"`
}
