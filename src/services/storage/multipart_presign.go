package storage

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7/pkg/s3utils"

	"minio-web/api/src/services/apperror"
)

// PresignMultipartPart signs only UploadPart for a previously controlled upload.
// Ownership, destination, expected size and session lifetime belong to the
// multipart service. The storage server authorizes and verifies the actual PUT.
// Content-Length is deliberately not signed: browsers set that forbidden header.
func (c *S3Client) PresignMultipartPart(ctx context.Context, bucket, key, uploadID string, partNumber int, checksumSHA256 string, expiry time.Duration) (*url.URL, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s3utils.CheckValidBucketName(bucket); err != nil {
		return nil, invalidMultipartSignature()
	}
	if err := s3utils.CheckValidObjectName(key); err != nil {
		return nil, invalidMultipartSignature()
	}
	digest, err := base64.StdEncoding.Strict().DecodeString(checksumSHA256)
	if err != nil || len(digest) != sha256.Size || base64.StdEncoding.EncodeToString(digest) != checksumSHA256 {
		return nil, invalidMultipartSignature()
	}
	if uploadID == "" || len(uploadID) > 4096 || strings.ContainsAny(uploadID, "\r\n\x00") || partNumber < 1 || partNumber > 10000 || expiry < time.Second || expiry > 5*time.Minute {
		return nil, invalidMultipartSignature()
	}
	query := url.Values{"uploadId": {uploadID}, "partNumber": {strconv.Itoa(partNumber)}}
	headers := http.Header{"X-Amz-Checksum-Sha256": {checksumSHA256}}
	signed, err := c.PresignHeader(ctx, http.MethodPut, bucket, key, expiry, query, headers)
	if err != nil {
		return nil, err
	}
	// Anonymous and V2 SDK credentials do not produce the required V4 header
	// binding. A syntactically valid URL alone is insufficient for this contract.
	if signed == nil || signed.Query().Get("X-Amz-Signature") == "" || signed.Query().Get("X-Amz-SignedHeaders") != "host;x-amz-checksum-sha256" {
		return nil, apperror.New(http.StatusConflict, "DirectUploadSigningUnavailable", "Не удалось подписать обязательные условия передачи части.")
	}
	return signed, nil
}

func invalidMultipartSignature() error {
	return apperror.New(http.StatusBadRequest, "InvalidMultipartPart", "Некорректные параметры подписи части загрузки.")
}
