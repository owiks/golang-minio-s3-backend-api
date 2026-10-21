package stream

import (
	"minio-web/api/src/http/presentation"
	"minio-web/api/src/services/result"
)

// Event is one localized wire record; services produce result.StreamEvent.
type Event[T any] struct {
	Progress *result.Progress    `json:"progress,omitempty"`
	Data     *result.ByteChunk   `json:"data,omitempty"`
	Result   *T                  `json:"result,omitempty"`
	Error    *presentation.Error `json:"error,omitempty"`
}
