// Package result contains small result envelopes shared by independent use cases.
package result

import "minio-web/api/src/services/apperror"

type Success struct {
	OK bool `json:"ok"`
}
type Link struct {
	URL string `json:"url"`
}

type Progress struct {
	Bytes int64 `json:"bytes"`
	Total int64 `json:"total"`
}

type StreamEvent[T any] struct {
	Progress *Progress       `json:"progress,omitempty"`
	Data     *ByteChunk      `json:"data,omitempty"`
	Result   *T              `json:"result,omitempty"`
	Error    *apperror.Error `json:"error,omitempty"`
}

// ByteChunk carries arbitrary bytes, not a complete text record. Consumers
// concatenate decoded bytes before interpreting UTF-8, CSV or JSON boundaries.
type ByteChunk struct {
	Encoding string `json:"encoding"`
	Bytes    string `json:"bytes"`
}
