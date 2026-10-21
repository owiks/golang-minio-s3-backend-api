package archives

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"strings"

	"minio-web/api/src/services/archives/zipread"
	"minio-web/api/src/services/messages"
	validationsvc "minio-web/api/src/services/validation"
)

const MaxZIPEntry int64 = 256 * 1024 * 1024

type ZipItem struct {
	Index      int          `json:"index"`
	Name       string       `json:"name"`
	Size       uint64       `json:"size"`
	Compressed uint64       `json:"compressed"`
	Folder     bool         `json:"folder"`
	Reason     messages.Ref `json:"reason,omitempty"`
}

func ZipEntryReason(f *zip.File) messages.Ref {
	return zipEntryReason(zipread.Entry{Name: f.Name, Size: f.UncompressedSize64, Mode: f.Mode(), Flags: f.Flags, Method: f.Method})
}

func zipEntryReason(f zipread.Entry) messages.Ref {
	name := strings.TrimSuffix(f.Name, "/")
	if name == "" || strings.HasPrefix(name, "/") || !validationsvc.ValidFileKey(name) || strings.Contains(name, ":") {
		return messages.ArchivesUnsafePath
	}
	if f.Mode&os.ModeSymlink != 0 {
		return messages.ArchivesSymbolicLink
	}
	if f.Flags&1 != 0 {
		return messages.ArchivesTheArchiveIsEncrypted
	}
	if f.Method != zip.Store && f.Method != zip.Deflate {
		return messages.ArchivesTheCompressionMethodIsNotSupported
	}
	if f.Size > uint64(MaxZIPEntry) {
		return messages.ArchivesTheFileExceeds256MiB
	}
	return nil
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
