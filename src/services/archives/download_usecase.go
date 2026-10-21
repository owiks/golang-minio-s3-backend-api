package archives

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	archiveplan "github.com/owiks/golang-minio-s3-backend-api/src/services/archives/plan"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	objectstate "github.com/owiks/golang-minio-s3-backend-api/src/services/objects/state"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/result"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/telemetry"
)

type ArchiveRequest struct {
	Name         string `json:"name"`
	Base         string `json:"base"`
	AllowPartial bool   `json:"allowPartial"`
}

func availablePlan(store ReadArchiveStore, owner, ticket string) (*archiveplan.ArchivePlan, error) {
	plan := store.Get(ticket, owner)
	if plan == nil || !plan.Available(owner, time.Now()) {
		return nil, apperror.New(404, "ArchiveExpired", messages.ArchivesArchiveExpired)
	}
	return plan, nil
}
func ArchiveStatus(store ReadArchiveStore, owner, ticket string) (archiveplan.ArchiveSnapshot, error) {
	plan, err := availablePlan(store, owner, ticket)
	if err != nil {
		return archiveplan.ArchiveSnapshot{}, err
	}
	return plan.Snapshot(ticket), nil
}
func ForgetArchive(store ForgetArchiveStore, owner, ticket string) (result.Success, error) {
	plan, err := availablePlan(store, owner, ticket)
	if err != nil {
		return result.Success{}, err
	}
	if err = plan.PrepareForget(); err != nil {
		return result.Success{}, err
	}
	store.Forget(ticket, plan)
	return result.Success{OK: true}, nil
}
func CancelArchive(store ReadArchiveStore, owner, ticket string) (archiveplan.ArchiveSnapshot, error) {
	plan, err := availablePlan(store, owner, ticket)
	if err != nil {
		return archiveplan.ArchiveSnapshot{}, err
	}
	return plan.Cancel(ticket), nil
}
func ConfigureArchive(store ReadArchiveStore, owner, ticket string, body ArchiveRequest) (archiveplan.ArchiveSnapshot, error) {
	plan, err := availablePlan(store, owner, ticket)
	if err != nil {
		return archiveplan.ArchiveSnapshot{}, err
	}
	return plan.Configure(ticket, body.Name, body.Base, body.AllowPartial)
}

type Download struct {
	mu                      sync.Mutex
	streamStarted, finished bool
	ctx                     context.Context
	cancel                  context.CancelFunc
	plan                    *archiveplan.ArchivePlan
	items                   []archiveplan.ArchiveItem
	name, ticket            string
	session                 DownloadReader
}

func (d *Download) Context() context.Context { return d.ctx }
func (d *Download) Name() string             { return d.name }

// Close cancels an active stream; that stream retains ownership until it exits.
// If streaming never started, Close also completes the plan immediately.
func (d *Download) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.finished {
		d.cancel()
		return
	}
	d.plan.Cancel(d.ticket)
	d.cancel()
	if !d.streamStarted {
		d.finishLocked(false)
	}
}

// finishLocked runs once. Context cleanup stays in Close: the HTTP owner first
// detaches its cancellation watcher so a finished ZIP is not interrupted.
func (d *Download) finishLocked(finalized bool) {
	ctxErr := d.ctx.Err()
	if ctxErr != nil {
		d.plan.Cancel(d.ticket)
	}
	outcome, code := d.plan.Finish(ctxErr, finalized)
	d.finished = true
	telemetry.Outcome(d.ctx, outcome, code)
}
func StartDownload(parent context.Context, store ReadArchiveStore, s StartDownloadClient, ticket string) (*Download, error) {
	plan, err := availablePlan(store, s.SessionID(), ticket)
	if err != nil {
		return nil, err
	}
	ctx, snapshot, cancel, err := plan.BeginDownload(parent)
	if err != nil {
		return nil, err
	}
	return &Download{ctx: ctx, cancel: cancel, plan: plan, items: snapshot.Items, name: snapshot.Name, ticket: ticket, session: s}, nil
}
func (d *Download) StreamTo(target io.Writer, manifestView func(Manifest) any) error {
	if manifestView == nil {
		return apperror.New(500, "ManifestRendererMissing", messages.InternalError)
	}
	d.mu.Lock()
	if d.finished || d.streamStarted {
		d.mu.Unlock()
		if err := d.ctx.Err(); err != nil {
			return err
		}
		return apperror.New(409, "ArchiveStarted", messages.ArchivesArchiveStarted)
	}
	d.streamStarted = true
	d.mu.Unlock()
	ctx, plan, items, ticket, s := d.ctx, d.plan, d.items, d.ticket, d.session
	finalized := false
	setResult := func(i int, state string, message messages.Ref) {
		if state == "done" {
			plan.CompleteItem(i)
		} else {
			plan.FailItem(i, message)
		}
		items[i].State, items[i].Message = state, message
	}
	defer func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.finishLocked(finalized)
	}()
	zw := zip.NewWriter(target)
	for i, item := range items {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if item.State == "error" {
			continue
		}
		opts := minio.GetObjectOptions{}
		_ = opts.SetMatchETag(item.Info.ETag)
		object, err := s.GetObject(ctx, plan.Bucket(), item.Key, opts)
		var info minio.ObjectInfo
		if err == nil {
			info, err = object.Stat()
		}
		if err != nil {
			if object != nil {
				object.Close()
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			setResult(i, "error", apperror.Public(err).Message)
			continue
		}
		if objectstate.DeletionRevision(objectstate.DeletionSnapshot{ETag: info.ETag, Size: info.Size, Modified: info.LastModified, Version: info.VersionID}) != objectstate.DeletionRevision(item.Info) {
			object.Close()
			setResult(i, "error", messages.ArchivesTheFileChangedAfterVerificationPrepareA)
			continue
		}
		header := &zip.FileHeader{Name: item.Path, Method: zip.Store}
		header.SetModTime(item.Info.Modified)
		header.SetMode(0600)
		writer, err := zw.CreateHeader(header)
		if err != nil {
			object.Close()
			return err
		}
		progress := &ArchiveProgressWriter{Writer: writer, Add: func(n int64) { plan.AddSent(i, n) }}
		written, err := io.Copy(progress, object)
		object.Close()
		if progress.Failed || ctx.Err() != nil {
			return io.ErrUnexpectedEOF
		}
		if err != nil || written != item.Size {
			setResult(i, "error", messages.ArchivesCouldNotReadTheEntireFileDo)
		} else {
			setResult(i, "done", nil)
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	manifest := ".minio-web-results-" + ticket[:8] + ".json"
	for _, item := range items {
		if item.Path == manifest {
			manifest = ".minio-web-results-" + ticket + ".json"
			break
		}
	}
	file, err := zw.Create(manifest)
	if err != nil {
		return err
	}
	snapshot := plan.Snapshot(ticket)
	items = snapshot.Items
	if err = json.NewEncoder(file).Encode(manifestView(Manifest{Bucket: plan.Bucket(), Files: items, Issues: snapshot.Issues})); err != nil {
		return err
	}
	err = zw.Close()
	finalized = err == nil
	if err != nil {
		return err
	}
	return nil
}

// Manifest is rendered by the caller at delivery time; the plan stores no language.
type Manifest struct {
	Bucket string                    `json:"bucket"`
	Files  []archiveplan.ArchiveItem `json:"files"`
	Issues map[string]messages.Ref   `json:"issues"`
}
