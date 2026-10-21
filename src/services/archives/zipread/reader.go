package zipread

import (
	"archive/zip"
	"context"
	"encoding/binary"
	"io"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/objects/state"
	"minio-web/api/src/services/staging"
)

const MaxStoredZIP int64 = 256 * 1024 * 1024

// Bound the central directory before archive/zip allocates one record per entry.
func CheckedZIP(f io.ReaderAt, size int64) (*zip.Reader, error) {
	if size < 22 {
		return nil, apperror.New(400, "InvalidZIP", "Некорректный ZIP.")
	}
	tail := make([]byte, min(size, 65557))
	if _, err := f.ReadAt(tail, size-int64(len(tail))); err != nil {
		return nil, err
	}
	found := false
	for i := len(tail) - 22; i >= 0; i-- {
		if binary.LittleEndian.Uint32(tail[i:]) != 0x06054b50 || i+22+int(binary.LittleEndian.Uint16(tail[i+20:])) != len(tail) {
			continue
		}
		found = true
		entries := binary.LittleEndian.Uint16(tail[i+10:])
		central := binary.LittleEndian.Uint32(tail[i+12:])
		offset := binary.LittleEndian.Uint32(tail[i+16:])
		if entries > 10000 || central > 16*1024*1024 || entries == 65535 || offset == 0xffffffff || int64(offset)+int64(central) > size-int64(len(tail))+int64(i) || binary.LittleEndian.Uint16(tail[i+4:]) != 0 || binary.LittleEndian.Uint16(tail[i+6:]) != 0 {
			return nil, apperror.New(413, "ZIPLimit", "Поддерживаются обычные ZIP до 10 000 записей, без ZIP64 и разделения на тома.")
		}
		directory := make([]byte, central)
		if _, err := f.ReadAt(directory, int64(offset)); err != nil {
			return nil, apperror.New(400, "InvalidZIP", "Некорректный каталог ZIP.")
		}
		count := 0
		for pos := 0; pos < len(directory); {
			if pos+46 > len(directory) || binary.LittleEndian.Uint32(directory[pos:]) != 0x02014b50 {
				return nil, apperror.New(400, "InvalidZIP", "Некорректный каталог ZIP.")
			}
			count++
			if count > 10000 {
				return nil, apperror.New(413, "ZIPLimit", "Слишком много записей в ZIP.")
			}
			step := 46 + int(binary.LittleEndian.Uint16(directory[pos+28:])) + int(binary.LittleEndian.Uint16(directory[pos+30:])) + int(binary.LittleEndian.Uint16(directory[pos+32:]))
			if pos+step > len(directory) {
				return nil, apperror.New(400, "InvalidZIP", "Некорректный каталог ZIP.")
			}
			pos += step
		}
		if count != int(entries) {
			return nil, apperror.New(400, "InvalidZIP", "Количество записей ZIP не совпало.")
		}

		break
	}
	if !found {
		return nil, apperror.New(400, "InvalidZIP", "Не найдена структура ZIP.")
	}
	z, err := zip.NewReader(f, size)
	if err != nil {
		return nil, apperror.New(400, "InvalidZIP", "Не удалось прочитать ZIP.")
	}
	if len(z.File) > 10000 {
		return nil, apperror.New(413, "ZIPLimit", "Слишком много записей в ZIP.")
	}
	return z, nil
}

// LoadStoredZIP uses a caller-owned budget, including on failed cleanup.
func LoadStoredZIP(ctx context.Context, s ObjectReadClient, bucket, key string, budget *staging.Budget) (*staging.File, *Index, string, error) {
	info, err := s.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return nil, nil, "", err
	}
	rev := objectstate.DeletionRevision(objectstate.DeletionSnapshot{ETag: info.ETag, Size: info.Size, Modified: info.LastModified, Version: info.VersionID})
	if info.Size > MaxStoredZIP {
		return nil, nil, "", apperror.New(413, "ZIPLimit", "Просмотр поддерживает ZIP до 256 МиБ.")
	}
	opts := minio.GetObjectOptions{}
	_ = opts.SetMatchETag(info.ETag)
	object, err := s.GetObject(ctx, bucket, key, opts)
	if err != nil {
		return nil, nil, "", err
	}
	defer object.Close()
	f, err := budget.Create(info.Size, "minio-zip-*")
	if err != nil {
		return nil, nil, "", err
	}
	ok := false
	defer func() {
		if !ok {
			_ = f.Close() // The budget retains and retries failed unlink.
		}
	}()
	n, err := io.Copy(f, io.LimitReader(object, MaxStoredZIP+1))
	if err != nil {
		return nil, nil, "", err
	}
	if n != info.Size || n > MaxStoredZIP {
		return nil, nil, "", apperror.New(409, "ZIPChanged", "Архив изменился. Обновите список.")
	}
	z, err := ReadIndex(f, n)
	if err != nil {
		return nil, nil, "", err
	}
	ok = true
	return f, z, rev, nil
}
