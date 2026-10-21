package archives

import (
	"context"
	"io"
	"mime"
	"path"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
	objectssvc "minio-web/api/src/services/objects"
	objectwrite "minio-web/api/src/services/objects/write"
	"minio-web/api/src/services/staging"
	validationsvc "minio-web/api/src/services/validation"
)

func InspectZIP(ctx context.Context, cache ZIPArchiveCache, s InspectZIPClient, bucket, key string) (ZIPContents, error) {
	bucket, key, err := objectssvc.ObjectParams(bucket, key)
	if err != nil {
		return ZIPContents{}, err
	}

	z, err := cache.Open(ctx, s, bucket, key)
	if err != nil {
		return ZIPContents{}, err
	}
	defer z.Close()
	entries, err := z.Entries()
	if err != nil {
		return ZIPContents{}, err
	}
	items := make([]ZipItem, 0, len(entries))
	for _, entry := range entries {
		items = append(items, ZipItem{Index: entry.Index, Name: entry.Name, Size: entry.Size, Compressed: entry.Compressed, Folder: entry.Mode.IsDir(), Reason: zipEntryReason(entry)})
	}
	return ZIPContents{Revision: z.Revision(), Items: items}, nil
}

func ExtractZIPEntry(ctx context.Context, cache ZIPArchiveCache, budget *staging.Budget, s ExtractZIPEntryClient, bucket, key string, body ZipExplorerRequest) (ExtractedEntry, error) {
	bucket, key, err := objectssvc.ObjectParams(bucket, key)
	if err != nil {
		return ExtractedEntry{}, err
	}

	if body.Bucket == "" || len(body.Bucket) > 63 || !validationsvc.ValidFileKey(body.Key) {
		return ExtractedEntry{}, apperror.New(400, "InvalidDestination", messages.ArchivesInvalidDestination)
	}

	z, err := cache.Open(ctx, s, bucket, key)
	if err != nil {
		return ExtractedEntry{}, err
	}
	defer z.Close()

	if body.Revision != z.Revision() {
		return ExtractedEntry{}, apperror.New(409, "ZIPChanged", messages.ArchivesZIPChanged)
	}
	entry, err := z.Entry(body.Index)
	if err != nil {
		return ExtractedEntry{}, err
	}
	if reason := zipEntryReason(entry); !messages.Empty(reason) {
		return ExtractedEntry{}, apperror.New(400, "UnsafeZIPEntry", reason)
	}
	if entry.Mode.IsDir() {
		return ExtractedEntry{}, apperror.New(400, "ZIPDirectory", messages.ArchivesZIPDirectory)
	}
	current, err := objectwrite.DestinationState(ctx, s, body.Bucket, body.Key)
	if err != nil {
		return ExtractedEntry{}, err
	}
	opts, err := objectwrite.WriteOptions(current, body.DestinationRevision, mime.TypeByExtension(path.Ext(entry.Name)))
	if err != nil {
		return ExtractedEntry{}, err
	}
	// CRC is checked before PUT, including when overwriting a destination.
	source, err := z.OpenEntry(body.Index)
	if err != nil {
		return ExtractedEntry{}, apperror.New(400, "CorruptZIPEntry", messages.ArchivesCorruptZIPEntry)
	}
	defer source.Close()
	staged, err := budget.Create(int64(entry.Size), "minio-extract-*")
	if err != nil {
		return ExtractedEntry{}, err
	}
	defer staged.Close()
	n, err := io.Copy(staged, io.LimitReader(ContextReader{Ctx: ctx, Source: source}, MaxZIPEntry+1))
	if err != nil || n != int64(entry.Size) || n > MaxZIPEntry {
		return ExtractedEntry{}, apperror.New(400, "CorruptZIPEntry", messages.ArchivesCorruptZIPEntryZipExplorerUsecase)
	}
	if _, err = staged.Seek(0, 0); err != nil {
		return ExtractedEntry{}, err
	}
	if _, err = s.PutObject(ctx, body.Bucket, body.Key, staged, n, opts); err != nil {
		return ExtractedEntry{}, objectwrite.FileWriteError(err)
	}
	return ExtractedEntry{OK: true, Size: n}, nil
}

type ZipExplorerRequest struct {
	Revision            string `json:"revision"`
	Index               int    `json:"index"`
	Bucket              string `json:"bucket"`
	Key                 string `json:"key"`
	DestinationRevision string `json:"destinationRevision"`
}
