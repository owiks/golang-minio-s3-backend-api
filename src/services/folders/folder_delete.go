package folders

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	searchsvc "minio-web/api/src/services/search"
)

const MaxFolderDeleteObjects = 5000

type FolderDeleteItem struct {
	Key    string `json:"key"`
	Size   int64  `json:"size"`
	Marker bool   `json:"marker"`
}

func FolderDeleteParams(bucket, prefix string) (string, string, error) {
	if bucket == "" || len(bucket) > 63 || prefix == "" || !strings.HasSuffix(prefix, "/") || len(prefix) > 1024 || !utf8.ValidString(prefix) || strings.ContainsFunc(prefix, unicode.IsControl) {
		return "", "", apperror.New(400, "InvalidFolderDelete", "Выберите конкретную папку. Удаление всего бакета или отдельных версий этим запросом не выполняется.")
	}
	return bucket, prefix, nil
}
func FolderDeleteItems(ctx context.Context, s FolderDeleteItemsClient, bucket, prefix string) ([]FolderDeleteItem, error) {
	items := []FolderDeleteItem{}
	token := ""
	guard := searchsvc.NewListingGuard(prefix, MaxFolderDeleteObjects+1)
	seen := map[string]bool{}
	for {
		page, err := searchsvc.RecursivePage(ctx, s, bucket, prefix, token)
		if err != nil {
			return nil, err
		}
		if err := guard.Check(ctx, token, page); err != nil {
			if apperror.Public(err).Code == "InvalidObjectListing" {
				return nil, apperror.New(502, "InvalidFolderListing", "Не удалось подтвердить полный список папки. Повторите проверку.")
			}
			return nil, err
		}
		for _, obj := range page.Contents {
			// Never normalize or broaden an opaque S3 prefix (a/ must not match a-backup/).
			if !strings.HasPrefix(obj.Key, prefix) {
				return nil, apperror.New(502, "InvalidFolderListing", "MinIO вернул объект за пределами выбранной папки. Не удалось подтвердить полный список.")
			}
			if seen[obj.Key] {
				continue
			}
			seen[obj.Key] = true
			items = append(items, FolderDeleteItem{Key: obj.Key, Size: obj.Size, Marker: strings.HasSuffix(obj.Key, "/") && obj.Size == 0})
			if len(items) > MaxFolderDeleteObjects {
				return nil, apperror.New(400, "FolderDeleteLimit", "В папке больше 5000 объектов. Выберите меньшую вложенную папку.")
			}
		}
		if !page.IsTruncated {
			return items, nil
		}
		token = page.NextContinuationToken
	}
}
func FolderMarkerHasChildren(ctx context.Context, s FolderExistsClient, bucket, key string) (bool, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	for obj := range s.ListObjects(ctx, bucket, minio.ListObjectsOptions{Prefix: key, Recursive: true, MaxKeys: 2}) {
		if obj.Err != nil {
			return false, obj.Err
		}
		if obj.Key != key {
			return true, nil
		}
	}
	return false, nil
}
