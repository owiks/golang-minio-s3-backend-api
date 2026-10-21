package uploads

import (
	"context"
	"errors"
	"io"
	"time"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/staging"
)

const MaxRemoteSize int64 = 1024 * 1024 * 1024

// Stage a bounded source before committing the destination; cancellation and an
// incomplete HTTP response cannot leave a seemingly successful truncated object.
// The caller owns the shared budget and its cleanup lifecycle.
func StageRemote(ctx context.Context, remote RemoteSource, raw string, progress func(int64, int64), budget *staging.Budget) (*staging.File, int64, string, error) {
	resp, err := remote.Open(ctx, raw)
	if err != nil {
		return nil, 0, "", err
	}
	defer resp.Body.Close()
	if resp.Size > MaxRemoteSize {
		return nil, 0, "", apperror.New(413, "ImportTooLarge", "Импорт поддерживает файлы до 1 ГиБ.")
	}
	reservation := resp.Size
	if reservation < 0 {
		reservation = MaxRemoteSize
	}
	f, err := budget.Create(reservation, "minio-import-*")
	if err != nil {
		return nil, 0, "", err
	}
	ok := false
	defer func() {
		if !ok {
			_ = f.Close() // The budget retains and retries failed unlink.
		}
	}()
	total := resp.Size
	if total < 0 {
		total = MaxRemoteSize
	}
	var count int64
	buf := make([]byte, 128*1024)
	last := time.Now()
	for {
		if err = ctx.Err(); err != nil {
			return nil, 0, "", err
		}
		n, e := resp.Body.Read(buf)
		count += int64(n)
		if count > MaxRemoteSize {
			return nil, 0, "", apperror.New(413, "ImportTooLarge", "Импорт поддерживает файлы до 1 ГиБ.")
		}
		if n > 0 {
			if _, err = f.Write(buf[:n]); err != nil {
				return nil, 0, "", err
			}
		}
		if time.Since(last) > 250*time.Millisecond {
			progress(count, max(total, count))
			last = time.Now()
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			if ctx.Err() != nil {
				return nil, 0, "", ctx.Err()
			}
			if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
				return nil, 0, "", e
			}
			return nil, 0, "", apperror.NewWithCause(502, "IncompleteSource", "Источник прервал передачу. Файл не записан.", e)
		}
	}
	if resp.Size >= 0 && count != resp.Size {
		return nil, 0, "", apperror.New(502, "IncompleteSource", "Источник вернул неполный файл.")
	}
	_, err = f.Seek(0, 0)
	if err != nil {
		return nil, 0, "", err
	}
	progress(count, count)
	ok = true
	return f, count, resp.ContentType, nil
}
