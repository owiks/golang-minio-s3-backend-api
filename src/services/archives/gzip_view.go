package archives

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"unicode/utf8"

	"minio-web/api/src/services/apperror"
)

const MaxGzipInput int64 = 32 << 20
const MaxGzipText int64 = 8 << 20

func DecodeGzipText(ctx context.Context, data []byte) (string, error) {
	z, e := gzip.NewReader(bytes.NewReader(data))
	if e != nil {
		return "", apperror.New(415, "InvalidGzip", "Файл не является корректным GZip.")
	}
	defer z.Close()
	decoded, e := io.ReadAll(io.LimitReader(&CancelReader{Ctx: ctx, R: z}, MaxGzipText+1))
	if e != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", apperror.New(415, "InvalidGzip", "Не удалось распаковать GZip: повреждён поток или контрольная сумма.")
	}
	if int64(len(decoded)) > MaxGzipText {
		return "", apperror.New(413, "GzipTextTooLarge", "После распаковки файл превышает 8 МиБ.")
	}
	if bytes.Count(decoded, []byte{'\n'}) >= 200000 {
		return "", apperror.New(413, "GzipTooManyLines", "После распаковки файл содержит больше 200 000 строк.")
	}
	if !utf8.Valid(decoded) || bytes.IndexByte(decoded, 0) >= 0 {
		return "", apperror.New(415, "NotUTF8Text", "После распаковки нужен текст UTF-8 без нулевых байтов.")
	}
	return string(decoded), nil
}

type CancelReader struct {
	Ctx context.Context `json:"-"`
	R   io.Reader       `json:"-"`
}

func (c *CancelReader) Read(p []byte) (int, error) {
	if e := c.Ctx.Err(); e != nil {
		return 0, e
	}
	return c.R.Read(p)
}
