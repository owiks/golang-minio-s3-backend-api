package integrity

import (
	"context"
	"io"
	"math"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
)

// Hash reads at most size+1 bytes with a bounded buffer and checks both premature
// EOF and surplus input. It does not close the caller-owned reader.
func Hash(ctx context.Context, source io.Reader, size int64, spec Spec) (Spec, error) {
	if spec.Type != FullObject || size < 0 || size == math.MaxInt64 {
		return Spec{}, UnsupportedMode()
	}
	h, err := NewHasher(spec.Algorithm)
	if err != nil {
		return Spec{}, err
	}
	if err = ctx.Err(); err != nil {
		return Spec{}, err
	}
	n, err := io.CopyBuffer(h, &contextReader{ctx: ctx, source: io.LimitReader(source, size+1)}, make([]byte, 128*1024))
	if err != nil {
		return Spec{}, err
	}
	if err = ctx.Err(); err != nil {
		return Spec{}, err
	}
	if n != size {
		return Spec{}, apperror.New(400, "IncompleteUpload", messages.UploadSizeMismatch)
	}
	return Value(spec, h.Sum(nil)), nil
}

type contextReader struct {
	ctx    context.Context
	source io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.source.Read(p)
}
