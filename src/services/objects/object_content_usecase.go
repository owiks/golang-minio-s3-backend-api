package objects

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/objects/write"
	"minio-web/api/src/services/result"
)

func GetContent(ctx context.Context, s GetContentClient, bucket, key string) (Content, error) {
	bucket, key, err := ObjectParams(bucket, key)
	if err != nil {
		return Content{}, err
	}
	kind, _ := PreviewKind(key)
	if kind != "text" {
		return Content{}, apperror.New(415, "NotEditable", "Редактор доступен для текстовых файлов.")
	}

	info, p, err := ReadProperties(ctx, s, bucket, key)
	if err != nil {
		return Content{}, err
	}
	if info.Size > MaxTextContent {
		return Content{}, apperror.New(413, "TextTooLarge", "Редактор и таблицы поддерживают файлы до 1 МиБ.")
	}
	if p.Headers["Content-Encoding"] != "" {
		return Content{}, apperror.New(415, "EncodedText", "Сжатое содержимое нельзя редактировать как обычный текст.")
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
		return Content{}, apperror.New(409, "ObjectChanged", "Файл изменился. Обновите содержимое.")
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return Content{}, apperror.New(415, "InvalidText", "Для просмотра нужен текст UTF-8 без нулевых байтов.")
	}
	return Content{Text: string(data), Revision: p.Revision, Writable: p.TagsError == "" && p.MetadataReason == "", Reason: p.TagsError + p.MetadataReason}, nil
}

func UpdateContent(ctx context.Context, s UpdateContentClient, bucket, key string, body ObjectContentRequest) (result.Success, error) {
	bucket, key, err := ObjectParams(bucket, key)
	if err != nil {
		return result.Success{}, err
	}
	kind, _ := PreviewKind(key)
	if kind != "text" {
		return result.Success{}, apperror.New(415, "NotEditable", "Редактор доступен для текстовых файлов.")
	}

	if int64(len(body.Text)) > MaxTextContent || !utf8.ValidString(body.Text) || strings.ContainsRune(body.Text, 0) {
		return result.Success{}, apperror.New(400, "InvalidText", "Нужен текст UTF-8 размером до 1 МиБ без нулевых байтов.")
	}
	if strings.HasSuffix(strings.ToLower(key), ".json") && !json.Valid([]byte(strings.TrimPrefix(body.Text, "\ufeff"))) {
		return result.Success{}, apperror.New(400, "InvalidDocument", "Исправьте ошибки JSON перед сохранением.")
	}

	info, p, err := ReadProperties(ctx, s, bucket, key)
	if err != nil {
		return result.Success{}, err
	}
	if info.Size > MaxTextContent {
		return result.Success{}, apperror.New(413, "TextTooLarge", "Редактор и таблицы поддерживают файлы до 1 МиБ.")
	}
	if p.Headers["Content-Encoding"] != "" {
		return result.Success{}, apperror.New(415, "EncodedText", "Сжатое содержимое нельзя редактировать как обычный текст.")
	}

	if body.Revision == "" || body.Revision != p.Revision {
		return result.Success{}, apperror.New(409, "ContentChanged", "Файл изменился в S3. Черновик сохранён в редакторе; загрузите актуальный файл для сравнения.")
	}
	if p.TagsError != "" || p.MetadataReason != "" {
		return result.Success{}, apperror.New(403, "ContentProtected", "Не удалось безопасно сохранить свойства или защиту файла. Редактирование недоступно.")
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
