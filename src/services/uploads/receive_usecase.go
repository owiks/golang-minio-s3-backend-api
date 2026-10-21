package uploads

import (
	"context"
	"io"
	"strings"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/coordination"
	"minio-web/api/src/services/objects/write"
	"minio-web/api/src/services/result"
	validationsvc "minio-web/api/src/services/validation"
)

func InspectUploadInvite(ctx context.Context, store UploadInviteReader, token string) (InviteInfo, error) {
	g := store.Get(token)
	if g == nil {
		return InviteInfo{}, apperror.New(404, "InviteExpired", "Ссылка недействительна или истекла.")
	}
	view, err := g.Inspect()
	if err != nil {
		return InviteInfo{}, err
	}
	result := InviteInfo{Bucket: view.Bucket, Prefix: view.Prefix, ExpiresAt: view.Expires, MaxSize: view.MaxSize, MaxFiles: view.MaxFiles, Uploaded: view.Uploaded}
	return result, nil
}

func ReceiveFile(ctx context.Context, store UploadInviteReader, admission coordination.TransferAdmission, token string, name, contentType string, size, contentLength int64, source io.Reader) (result.Success, error) {
	g := store.Get(token)
	if g == nil {
		return result.Success{}, apperror.New(404, "InviteExpired", "Ссылка недействительна или истекла.")
	}
	view, err := g.Inspect()
	if err != nil {
		return result.Success{}, err
	}
	if size < 0 || size > view.MaxSize || contentLength != size || name == "" || strings.ContainsAny(name, "/\\") || !validationsvc.ValidFileKey(view.Prefix+name) {
		return result.Success{}, apperror.New(400, "InvalidUpload", "Проверьте имя файла и ограничение размера.")
	}
	lease, err := g.BeginWrite(ctx)
	if err != nil {
		return result.Success{}, err
	}
	defer lease.Close()
	uploadCtx := lease.Context()
	release, admissionErr := admission.AcquireTransfer(g.Owner())
	if admissionErr != nil {
		return result.Success{}, admissionErr
	}
	defer release()
	opts, err := objectwrite.WriteOptions(objectwrite.FileState{Revision: "absent"}, "absent", contentType)
	if err != nil {
		return result.Success{}, err
	}
	_, err = g.Writer().PutObject(uploadCtx, view.Bucket, view.Prefix+name, io.LimitReader(source, view.MaxSize+1), size, opts)
	if err != nil {
		if mapped := apperror.Public(objectwrite.FileWriteError(err)); mapped.Code == "DestinationChanged" {
			return result.Success{}, apperror.New(409, "UploadExists", "Файл с таким именем уже существует. Переименуйте его и повторите загрузку.")
		}
		return result.Success{}, objectwrite.FileWriteError(err)
	}
	lease.Commit()
	return result.Success{OK: true}, nil
}
