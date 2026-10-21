package selectquery

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/capabilities"
	"minio-web/api/src/services/objects"
	"minio-web/api/src/services/result"
	"minio-web/api/src/services/storage"
)

// Run owns one prepared SDK stream. It is single-use and cancellation closes
// the upstream even while a slow or disconnected HTTP consumer is blocked.
type Run struct {
	mu              sync.Mutex
	started, closed bool
	ctx             context.Context
	cancel          context.CancelFunc
	client          Client
	reader          storage.SelectStream
	observer        capabilities.Observer
	feature         capabilities.Identifier
	bucket, key     string
	info            minio.ObjectInfo
	maxResultBytes  int64
	first           []byte
	firstErr        error
}

// Context exposes only cancellation/deadline observation. HTTP delivery uses it
// while emitting a chunk so a slow consumer cannot outlive this query's budget.
func (r *Run) Context() context.Context { return r.ctx }

func Prepare(parent context.Context, client Client, observer capabilities.Observer, bucket, key string, request Request) (*Run, error) {
	if _, _, err := objects.ObjectParams(bucket, key); err != nil {
		return nil, err
	}
	if strings.HasSuffix(key, "/") {
		return nil, apperror.New(400, "InvalidObject", "Выберите файл.")
	}
	options, request, err := validate(request)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(request.TimeoutSeconds)*time.Second)
	run := &Run{ctx: ctx, cancel: cancel, client: client, observer: observer, bucket: bucket, key: key, maxResultBytes: request.MaxResultBytes, feature: capabilities.SelectCSV}
	if request.Input.JSON != nil {
		run.feature = capabilities.SelectJSON
	}
	prepared := false
	defer func() {
		if !prepared {
			run.Close()
		}
	}()
	info, err := client.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return nil, err
	}
	if info.Size < 0 || info.Size > MaxSourceBytes {
		return nil, apperror.New(400, "SelectSourceTooLarge", "Для S3 Select выберите объект не больше 256 МиБ.")
	}
	run.info = info
	run.reader, err = client.OpenSelect(ctx, bucket, key, options)
	if err != nil {
		run.observe(err)
		return nil, selectError(err)
	}
	buffer := make([]byte, ChunkBytes)
	n, readErr := run.reader.Read(buffer)
	if readErr != nil && readErr != io.EOF {
		run.observe(readErr)
		return nil, selectError(readErr)
	}
	if int64(n) > request.MaxResultBytes {
		return nil, apperror.New(413, "SelectResultLimit", "Результат S3 Select превышает установленный лимит. Уточните запрос.")
	}
	run.first, run.firstErr = buffer[:n], readErr
	prepared = true
	return run, nil
}

func (r *Run) Close() {
	r.mu.Lock()
	r.closed = true
	reader, cancel := r.reader, r.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if reader != nil {
		_ = reader.Close()
	}
}

func (r *Run) observe(err error) {
	if r.observer != nil {
		r.observer.Observe(r.ctx, capabilities.Observation{ID: r.feature, Scope: capabilities.Scope{Bucket: r.bucket, Key: r.key}, Err: err})
	}
}

func selectError(err error) error {
	var safe *apperror.Error
	if errors.As(err, &safe) {
		return safe
	}
	native := minio.ToErrorResponse(err)
	code := native.Code
	if native.StatusCode == 400 {
		return apperror.New(400, "SelectQueryFailed", "Хранилище отклонило запрос S3 Select. Проверьте SQL и формат файла; результат неполный.")
	}
	switch code {
	case "InvalidExpression", "ParseExpectedTokenType", "ParseUnexpectedToken", "ParseInvalidToken", "LexerInvalidChar", "InvalidDataSource", "InvalidCompressionFormat", "InvalidFileHeaderInfo", "InvalidJsonType", "InvalidRequestParameter", "CSVParsingError", "JSONParsingError", "InvalidTextEncoding", "OverMaxRecordSize", "UnsupportedSqlOperation":
		return apperror.New(400, "SelectQueryFailed", "Хранилище отклонило запрос S3 Select. Проверьте SQL и формат файла; результат неполный.")
	default:
		return apperror.Public(err)
	}
}

func (r *Run) Stream(parent context.Context, emit func(result.StreamEvent[Result]) bool) error {
	r.mu.Lock()
	if r.started || r.closed || r.ctx == nil || r.reader == nil || r.client == nil {
		r.mu.Unlock()
		return apperror.New(409, "SelectClosed", "Запрос S3 Select уже запущен или закрыт.")
	}
	r.started = true
	r.mu.Unlock()
	defer r.Close()
	if err := parent.Err(); err != nil {
		r.cancel()
		return err
	}
	stop := context.AfterFunc(parent, r.cancel)
	defer stop()
	buffer := make([]byte, ChunkBytes)
	data, readErr := r.first, r.firstErr
	count := int64(0)
	for {
		if err := r.ctx.Err(); err != nil {
			r.observe(err)
			return selectError(err)
		}
		if len(data) > 0 {
			if count+int64(len(data)) > r.maxResultBytes {
				return apperror.New(413, "SelectResultLimit", "Результат S3 Select превысил установленный лимит. Уже полученные данные неполные.")
			}
			count += int64(len(data))
			if !emit(result.StreamEvent[Result]{Data: &result.ByteChunk{Encoding: "base64", Bytes: base64.StdEncoding.EncodeToString(data)}}) {
				r.cancel()
				return context.Canceled
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			r.observe(readErr)
			return selectError(readErr)
		}
		n, err := r.reader.Read(buffer)
		data, readErr = buffer[:n], err
	}
	// Select lacks a native version/precondition in this SDK. Recheck the
	// snapshot and reject a detectable replacement; this is not atomic pinning.
	current, err := r.client.StatObject(r.ctx, r.bucket, r.key, minio.StatObjectOptions{})
	if err != nil {
		return err
	}
	if current.Size != r.info.Size || current.ETag != r.info.ETag || current.VersionID != r.info.VersionID || !current.LastModified.Equal(r.info.LastModified) {
		return apperror.New(409, "SelectSourceChanged", "Файл изменился во время S3 Select. Результат неполный; повторите запрос.")
	}
	value := Result{Complete: true, Bytes: count}
	if stats := r.reader.FinalStats(); stats != nil {
		if stats.BytesReturned != count || stats.BytesScanned < 0 || stats.BytesProcessed < 0 {
			err := apperror.New(502, "InvalidSelectStatistics", "Итоговые сведения S3 Select не соответствуют полученному результату. Результат неполный.")
			r.observe(err)
			return err
		}
		value.Statistics = &Statistics{BytesScanned: stats.BytesScanned, BytesProcessed: stats.BytesProcessed, BytesReturned: stats.BytesReturned}
	}
	if err := r.ctx.Err(); err != nil {
		return err
	}
	r.observe(nil)
	if !emit(result.StreamEvent[Result]{Result: &value}) {
		r.cancel()
		return context.Canceled
	}
	return nil
}
