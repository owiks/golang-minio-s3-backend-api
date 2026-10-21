package stream

import (
	"github.com/owiks/golang-minio-s3-backend-api/src/http/presentation"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/result"
)

// Event is one localized wire record; services produce result.StreamEvent.
type Event[T any] struct {
	Progress *result.Progress    `json:"progress,omitempty"`
	Data     *result.ByteChunk   `json:"data,omitempty"`
	Result   *T                  `json:"result,omitempty"`
	Error    *presentation.Error `json:"error,omitempty"`
}
