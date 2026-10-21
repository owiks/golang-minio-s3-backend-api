package folders

import (
	"context"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/coordination"
	"minio-web/api/src/services/messages"
	objectssvc "minio-web/api/src/services/objects"
	"minio-web/api/src/services/result"
)

type PublicFolderOptions struct {
	Mode, Prefix, Cursor, Key, Kind string
}

func ReadPublicFolder(ctx context.Context, store PublicFolderStore, admission coordination.TransferAdmission, token string, input PublicFolderOptions) (PublicFolderResult, error) {
	g := store.Get(token)
	if g == nil {
		return PublicFolderResult{}, apperror.New(404, "FolderShareExpired", messages.FoldersFolderShareExpired)
	}
	ctx, release, err := g.BeginRead(ctx, admission)
	if err != nil {
		return PublicFolderResult{}, err
	}
	defer release()
	switch input.Mode {
	case "", "info":
		return PublicFolderResult{Info: &PublicFolderInfo{Bucket: g.Bucket(), Prefix: g.Prefix(), ExpiresAt: g.Expires()}}, nil
	case "list":
		prefix, cursor := input.Prefix, input.Cursor
		if !SharePrefix(prefix) || !strings.HasPrefix(prefix, g.Prefix()) || len(cursor) > 8192 {
			return PublicFolderResult{}, apperror.New(403, "FolderScopeDenied", messages.FoldersFolderScopeDenied)
		}
		page, e := ScopedObjectPage(ctx, g.Reader(), g.Bucket(), prefix, cursor, "/", 200)
		if e != nil {
			return PublicFolderResult{}, e
		}
		rows := []objectssvc.Entry{}
		for _, folder := range page.CommonPrefixes {
			if strings.HasPrefix(folder.Prefix, g.Prefix()) {
				rows = append(rows, objectssvc.Entry{Key: folder.Prefix, Name: strings.TrimSuffix(strings.TrimPrefix(folder.Prefix, prefix), "/"), Kind: "folder"})
			}
		}
		for _, obj := range page.Contents {
			if obj.Key == prefix && obj.Size == 0 || !g.Contains(obj.Key) {
				continue
			}
			date := obj.LastModified
			rows = append(rows, objectssvc.Entry{Key: obj.Key, Name: strings.TrimPrefix(obj.Key, prefix), Kind: "file", Size: obj.Size, Modified: &date})
		}
		next := ""
		if page.IsTruncated {
			next = page.NextContinuationToken
		}
		return PublicFolderResult{Page: &objectssvc.ObjectPage{Entries: rows, NextToken: next}}, nil
	case "preview", "ticket":
		key := input.Key
		if !g.Contains(key) || strings.HasSuffix(key, "/") {
			return PublicFolderResult{}, apperror.New(403, "FolderScopeDenied", messages.FoldersFolderScopeDeniedPublicUsecase)
		}
		info, e := g.Reader().StatObject(ctx, g.Bucket(), key, minio.StatObjectOptions{})
		if e != nil {
			return PublicFolderResult{}, e
		}
		if input.Mode == "ticket" {
			mode := input.Kind
			if mode != "download" && mode != "media" {
				return PublicFolderResult{}, apperror.New(400, "InvalidTicket", messages.FoldersInvalidTicket)
			}
			kind, _ := objectssvc.PreviewKind(key)
			if mode == "media" && kind != "audio" && kind != "video" {
				return PublicFolderResult{}, apperror.New(415, "UnsupportedPreview", messages.FoldersUnsupportedPreview)
			}
			id, err := store.IssueTicket(g, key, mode)
			if err != nil {
				return PublicFolderResult{}, err
			}
			return PublicFolderResult{Link: &result.Link{URL: "/api/shared-folder/file/" + id}}, nil
		}
		kind, contentType := objectssvc.PreviewKind(key)
		limit := int64(20 * 1024 * 1024)
		if kind == "text" {
			limit = objectssvc.MaxTextContent
		}
		if kind != "text" && kind != "image" && kind != "pdf" {
			return PublicFolderResult{}, apperror.New(415, "UnsupportedPreview", messages.FoldersUnsupportedPreviewPublicUsecase)
		}
		if info.Size > limit {
			return PublicFolderResult{}, apperror.New(413, "PreviewTooLarge", messages.FoldersPreviewTooLarge)
		}
		opts := minio.GetObjectOptions{}
		_ = opts.SetMatchETag(info.ETag)
		object, e := g.Reader().GetObject(ctx, g.Bucket(), key, opts)
		if e != nil {
			return PublicFolderResult{}, e
		}
		defer object.Close()
		data, e := io.ReadAll(io.LimitReader(object, limit+1))
		if e != nil {
			return PublicFolderResult{}, e
		}
		if int64(len(data)) > limit || int64(len(data)) != info.Size {
			return PublicFolderResult{}, apperror.New(409, "ObjectChanged", messages.FilesObjectChanged)
		}
		if kind == "text" && (!utf8.Valid(data) || strings.ContainsRune(string(data), 0)) {
			return PublicFolderResult{}, apperror.New(415, "InvalidText", messages.FoldersInvalidText)
		}
		return PublicFolderResult{Preview: &objectssvc.PreviewData{ContentType: contentType, Data: data}}, nil

	}
	return PublicFolderResult{}, apperror.New(404, "NotFound", messages.FoldersNotFound)
}

func OpenSharedFile(ctx context.Context, store SharedFileTickets, admission coordination.TransferAdmission, id string, consume bool, rangeHeader string) (*SharedRead, error) {
	ticket, ok := store.TakeTicket(id, consume)
	if !ok || time.Now().After(ticket.Expires) {
		return nil, apperror.New(410, "DownloadExpired", messages.FoldersDownloadExpired)
	}
	g := ticket.Share
	ctx, release, err := g.BeginRead(ctx, admission)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	keep := false
	defer func() {
		if !keep {
			cancel()
			release()
		}
	}()
	if !g.Contains(ticket.Key) {
		return nil, apperror.New(403, "FolderScopeDenied", messages.FoldersFolderScopeDeniedPublicUsecaseOpenSharedFile)
	}
	info, err := g.Reader().StatObject(ctx, g.Bucket(), ticket.Key, minio.StatObjectOptions{})
	if err != nil {
		return nil, err
	}
	opts := minio.GetObjectOptions{}
	_ = opts.SetMatchETag(info.ETag)
	start, end := int64(0), info.Size-1
	status := 200
	if raw := rangeHeader; ticket.Mode == "media" && raw != "" {
		var e error
		start, end, e = ParseShareRange(raw, info.Size)
		if e != nil {
			return nil, &RangeError{Size: info.Size, Err: e}
		}
		_ = opts.SetRange(start, end)
		status = 206
	}
	object, err := g.Reader().GetObject(ctx, g.Bucket(), ticket.Key, opts)
	if err != nil {
		return nil, err
	}

	if _, err = object.Stat(); err != nil {
		object.Close()
		return nil, err
	}

	contentType, disposition := "application/octet-stream", "attachment"
	if ticket.Mode == "media" {
		_, contentType = objectssvc.PreviewKind(ticket.Key)
		disposition = "inline"
	}
	keep = true
	return &SharedRead{object: object, release: release, ctx: ctx, cancel: cancel, info: SharedFileInfo{Size: info.Size, Start: start, End: end, Status: status, ContentType: contentType, Disposition: disposition, Name: ticket.Key[strings.LastIndex(ticket.Key, "/")+1:], Media: ticket.Mode == "media"}}, nil
}

type RangeError struct {
	Size int64
	Err  error
}

func (e *RangeError) Error() string { return e.Err.Error() }
func (e *RangeError) Unwrap() error { return e.Err }
