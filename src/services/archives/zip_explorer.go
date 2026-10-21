package archives

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"strings"

	"minio-web/api/src/services/archives/zipread"
	validationsvc "minio-web/api/src/services/validation"
)

const MaxZIPEntry int64 = 256 * 1024 * 1024

type ZipItem struct {
	Index      int    `json:"index"`
	Name       string `json:"name"`
	Size       uint64 `json:"size"`
	Compressed uint64 `json:"compressed"`
	Folder     bool   `json:"folder"`
	Reason     string `json:"reason,omitempty"`
}

func ZipEntryReason(f *zip.File) string {
	return zipEntryReason(zipread.Entry{Name: f.Name, Size: f.UncompressedSize64, Mode: f.Mode(), Flags: f.Flags, Method: f.Method})
}

func zipEntryReason(f zipread.Entry) string {
	name := strings.TrimSuffix(f.Name, "/")
	if name == "" || strings.HasPrefix(name, "/") || !validationsvc.ValidFileKey(name) || strings.Contains(name, ":") {
		return "Небезопасный путь"
	}
	if f.Mode&os.ModeSymlink != 0 {
		return "Символическая ссылка"
	}
	if f.Flags&1 != 0 {
		return "Архив зашифрован"
	}
	if f.Method != zip.Store && f.Method != zip.Deflate {
		return "Метод сжатия не поддерживается"
	}
	if f.Size > uint64(MaxZIPEntry) {
		return "Файл больше 256 МиБ"
	}
	return ""
}

type ContextReader struct {
	Ctx    context.Context `json:"-"`
	Source io.Reader       `json:"-"`
}

func (r ContextReader) Read(p []byte) (int, error) {
	if err := r.Ctx.Err(); err != nil {
		return 0, err
	}
	return r.Source.Read(p)
}
