package objects

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	objectwrite "github.com/owiks/golang-minio-s3-backend-api/src/services/objects/write"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/result"
)

func GetContent(ctx context.Context, s GetContentClient, bucket, key string) (Content, error) {
	bucket, key, err := ObjectParams(bucket, key)
	if err != nil {
		return Content{}, err
	}
	kind, _ := PreviewKind(key)
	if kind != "text" {
		return Content{}, apperror.New(415, "NotEditable", messages.ObjectsNotEditable)
	}

	info, p, err := ReadProperties(ctx, s, bucket, key)
	if err != nil {
		return Content{}, err
	}
	if info.Size > MaxTextContent {
		return Content{}, apperror.New(413, "TextTooLarge", messages.ObjectsTextTooLarge)
	}
	if p.Headers["Content-Encoding"] != "" {
		return Content{}, apperror.New(415, "EncodedText", messages.ObjectsEncodedText)
	}
	opts := minio.GetObjectOptions{VersionID: info.VersionID}
	_ = opts.SetMatchETag(info.ETag)
	object, e := s.GetObject(ctx, bucket, key, opts)
	if e != nil {
		return Content{}, propertyContentReadError(e)
	}
	data, e := io.ReadAll(io.LimitReader(object, MaxTextContent+1))
	_ = object.Close()
	if e != nil {
		return Content{}, propertyContentReadError(e)
	}
	if int64(len(data)) != info.Size || int64(len(data)) > MaxTextContent {
		return Content{}, apperror.New(409, "ObjectChanged", messages.ObjectsObjectChangedObjectContentUsecase)
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return Content{}, apperror.New(415, "InvalidText", messages.ObjectsInvalidText)
	}
	return Content{Text: string(data), Revision: p.Revision, Writable: messages.Empty(p.TagsError) && messages.Empty(p.MetadataReason), Reason: messages.Join(p.TagsError, p.MetadataReason)}, nil
}

func UpdateContent(ctx context.Context, s UpdateContentClient, bucket, key string, body ObjectContentRequest) (result.Success, error) {
	bucket, key, err := ObjectParams(bucket, key)
	if err != nil {
		return result.Success{}, err
	}
	kind, _ := PreviewKind(key)
	if kind != "text" {
		return result.Success{}, apperror.New(415, "NotEditable", messages.ObjectsNotEditable)
	}

	if int64(len(body.Text)) > MaxTextContent || !utf8.ValidString(body.Text) || strings.ContainsRune(body.Text, 0) {
		return result.Success{}, apperror.New(400, "InvalidText", messages.ObjectsInvalidTextObjectContentUsecase)
	}
	if strings.HasSuffix(strings.ToLower(key), ".json") && !json.Valid([]byte(strings.TrimPrefix(body.Text, "\ufeff"))) {
		return result.Success{}, apperror.New(400, "InvalidDocument", messages.ObjectsInvalidDocument)
	}

	info, p, err := ReadProperties(ctx, s, bucket, key)
	if err != nil {
		return result.Success{}, err
	}
	if info.Size > MaxTextContent {
		return result.Success{}, apperror.New(413, "TextTooLarge", messages.ObjectsTextTooLarge)
	}
	if p.Headers["Content-Encoding"] != "" {
		return result.Success{}, apperror.New(415, "EncodedText", messages.ObjectsEncodedText)
	}

	if body.Revision == "" || body.Revision != p.Revision {
		return result.Success{}, apperror.New(409, "ContentChanged", messages.ObjectsContentChanged)
	}
	if !messages.Empty(p.TagsError) || !messages.Empty(p.MetadataReason) {
		return result.Success{}, apperror.New(403, "ContentProtected", messages.ObjectsContentProtected)
	}
	_, err = s.PutObject(ctx, bucket, key, strings.NewReader(body.Text), int64(len(body.Text)), PreservedContentOptions(info, p))
	if err != nil {
		return result.Success{}, objectwrite.FileWriteError(err)
	}
	return result.Success{OK: true}, nil
}

type ObjectContentRequest struct {
	Revision string `json:"revision"`
	Text     string `json:"text"`
}
