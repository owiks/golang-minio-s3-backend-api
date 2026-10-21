package folders

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
)

func FolderKey(prefix, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || !utf8.ValidString(name) || strings.ContainsAny(name, "/\\") || strings.ContainsFunc(name, unicode.IsControl) {
		return "", apperror.New(400, "InvalidFolderName", messages.FoldersInvalidFolderName)
	}
	// Prefixes are opaque S3 keys. Preserve empty segments and never path.Clean them.
	if !utf8.ValidString(prefix) || prefix != "" && !strings.HasSuffix(prefix, "/") || strings.ContainsAny(prefix, "\\") || strings.ContainsFunc(prefix, unicode.IsControl) {
		return "", apperror.New(400, "InvalidFolderPath", messages.FoldersInvalidFolderPathFolders)
	}
	for _, part := range strings.Split(prefix, "/") {
		if part == "." || part == ".." {
			return "", apperror.New(400, "InvalidFolderPath", messages.FoldersInvalidFolderPathFoldersFolderKey)
		}
	}
	key := prefix + name + "/"
	if len(key) > 1024 {
		return "", apperror.New(400, "InvalidFolderName", messages.FoldersInvalidFolderNameFolders)
	}
	return key, nil
}

func FolderExists(ctx context.Context, s FolderExistsClient, bucket, key string) (bool, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// One object suffices, including an existing marker or a nested file. Cancel
	// the iterator immediately rather than enumerating a potentially large folder.
	for object := range s.ListObjects(ctx, bucket, minio.ListObjectsOptions{Prefix: key, Recursive: true, MaxKeys: 1}) {
		if object.Err != nil {
			return false, object.Err
		}
		return true, nil
	}
	return false, nil
}
func FolderConflict() error {
	return apperror.New(409, "FolderExists", messages.FoldersFolderExists)
}
