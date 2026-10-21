// Package apperror defines errors that are safe to expose to API clients.
package apperror

import (
	"context"
	"errors"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/upstream"
)

type Error struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
	cause   upstream.Cause
}

func (e *Error) Error() string { return e.Code }
func New(status int, code, message string) error {
	return &Error{Status: status, Code: code, Message: message}
}
func (e *Error) DiagnosticCause() upstream.Cause { return e.cause }

// NewWithCause retains only detached diagnostic metadata, never the raw error.
func NewWithCause(status int, code, message string, err error) error {
	return &Error{Status: status, Code: code, Message: message, cause: diagnosticCause(err)}
}
func diagnosticCause(err error) upstream.Cause {
	var own *Error
	if errors.As(err, &own) {
		return own.cause
	}
	return upstream.Classify(err, upstream.Operation)
}
func Public(err error) (result *Error) {
	var own *Error
	if errors.As(err, &own) {
		return own
	}
	defer func() { result.cause = diagnosticCause(err) }()
	code := minio.ToErrorResponse(err).Code
	var adminErr madmin.ErrorResponse
	if errors.As(err, &adminErr) {
		code = adminErr.Code
	}
	switch code {
	case "InvalidAccessKeyId", "SignatureDoesNotMatch":
		return &Error{Status: 403, Code: code, Message: "Не удалось проверить ключи. Проверьте Access Key, Secret Key, регион и часы устройства."}
	case "AccessDenied", "XMinioAdminAccessDenied":
		return &Error{Status: 403, Code: "AccessDenied", Message: "Недостаточно прав MinIO для этой операции."}
	case "NoSuchBucket", "NoSuchKey", "NoSuchUpload", "XMinioAdminNoSuchUser", "XMinioAdminNoSuchGroup", "XMinioAdminNoSuchPolicy", "XMinioAdminNoSuchServiceAccount":
		return &Error{Status: 404, Code: code, Message: "Ресурс больше не существует. Обновите список."}
	case "InvalidBucketState":
		return &Error{Status: 409, Code: code, Message: "Состояние бакета не позволяет применить эту настройку."}
	case "BucketNotEmpty":
		return &Error{Status: 409, Code: code, Message: "Бакет не пуст. Удаление объектов, версий и delete markers в этой операции не выполняется."}
	case "BadDigest", "XAmzContentSHA256Mismatch", "ChecksumMismatch":
		return &Error{Status: 400, Code: "ChecksumMismatch", Message: "Хранилище отклонило данные: контрольная сумма не совпадает. Проверьте состояние загрузки перед повтором."}
	case "InvalidDigest":
		return &Error{Status: 400, Code: "InvalidChecksum", Message: "Хранилище отклонило формат контрольной суммы. Проверьте алгоритм и кодировку."}
	case "BucketAlreadyExists", "BucketAlreadyOwnedByYou", "XMinioAdminGroupNotEmpty", "XMinioAdminPolicyInUse":
		return &Error{Status: 409, Code: code, Message: "Ресурс уже существует или используется. Обновите список и проверьте зависимости."}
	case "InvalidBucketName", "InvalidArgument", "MalformedPolicy", "XMinioAdminInvalidArgument", "XMinioAdminInvalidAccessKey", "XMinioAdminInvalidSecretKey", "EntityTooLarge":
		return &Error{Status: 400, Code: code, Message: "MinIO отклонил параметры. Проверьте имя, пароль или документ политики."}
	case "ExpiredToken", "InvalidToken":
		return &Error{Status: 403, Code: code, Message: "Токен MinIO недействителен или истёк. Подключитесь заново."}
	case "NotImplemented", "XMinioAdminNotImplemented":
		return &Error{Status: 501, Code: code, Message: "Эта операция не поддерживается версией или типом сервера."}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &Error{Status: 504, Code: "Timeout", Message: "S3 API не ответил вовремя. Обновите состояние перед повтором операции."}
	}
	return &Error{Status: 502, Code: "UpstreamError", Message: "Не удалось выполнить запрос к MinIO. Проверьте адрес, доступ из контейнера API, сертификат и права пользователя."}
}
