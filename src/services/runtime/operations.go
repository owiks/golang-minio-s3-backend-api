package runtime

import (
	"context"
	"log/slog"
	"math"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/operationpolicy"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/operations"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/search"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/transfers"
)

// AuthorizeOperation uses current API grants even for stored results. Neither
// a saved manifest nor possession of an operation ID grants resource access.
func (app *State) AuthorizeOperation(ctx context.Context, s *Session, spec operations.Spec, write bool) error {
	if !spec.Valid() {
		return operations.PublicError(operations.ErrInvalid)
	}
	p, err := s.livePolicy(ctx)
	if err != nil {
		return err
	}
	if spec.Scan != nil {
		if !p.Allows("objects:read") || !p.AllowsObject(spec.Scan.Bucket, spec.Scan.Prefix) {
			return authstate.Denied()
		}
		return nil
	}
	action := operationpolicy.Upload
	for _, item := range spec.Transfers {
		if !p.Allows("objects:read") || !p.AllowsObject(item.SourceBucket, item.SourceKey) || !p.Allows("objects:write") || !p.AllowsObject(item.DestinationBucket, item.DestinationKey) {
			return authstate.Denied()
		}
		if item.Move {
			action |= operationpolicy.Delete
			if !p.Allows("objects:delete") {
				return authstate.Denied()
			}
		}
	}
	if write {
		return app.config.Operations.Check(action)
	}
	return nil
}

type operationExecutor struct{ app *State }

func (e operationExecutor) session(ctx context.Context, job operations.Snapshot) (*Session, context.Context, error) {
	if !time.Now().Before(job.Deadline) {
		return nil, ctx, authstate.Denied()
	}
	a, c, err := e.app.Auth.Lookup(ctx, job.Scope.ActorID)
	if err != nil {
		return nil, ctx, err
	}
	if !a.Valid(time.Now()) || !c.Snapshot().Active || a.Snapshot().ConnectionID != job.Scope.ConnectionID || c.Snapshot().ID != job.Scope.ConnectionID {
		return nil, ctx, authstate.Denied()
	}
	s, err := e.app.BindAccess(ctx, a, c, false)
	if err != nil {
		return nil, ctx, err
	}
	// A later renewal must not extend the authorization captured at submission.
	ctx = authstate.WithCredential(ctx, s.id, a.Snapshot().Digest, job.Deadline)
	return s, ctx, nil
}

func (e operationExecutor) Authorize(ctx context.Context, job operations.Snapshot) error {
	s, ctx, err := e.session(ctx, job)
	if err != nil {
		return err
	}
	return e.app.AuthorizeOperation(ctx, s, job.Spec, true)
}

func (e operationExecutor) Transfer(ctx context.Context, job operations.Snapshot, input transfers.FileTransferRequest) (operations.TransferOutcome, error) {
	s, ctx, err := e.session(ctx, job)
	if err != nil {
		return operations.TransferOutcome{}, err
	}
	if err = e.app.AuthorizeOperation(ctx, s, job.Spec, true); err != nil {
		return operations.TransferOutcome{}, err
	}
	release, err := e.app.AcquireMutation(ctx, s, "objects/transfer")
	if err != nil {
		return operations.TransferOutcome{}, err
	}
	defer release()
	result, err := transfers.Transfer(ctx, NewClients(s), input)
	return operations.TransferOutcome{Copied: result.Copied, Moved: result.Moved, Code: result.Code, Uncertain: result.Code == "DeleteUncertain"}, err
}

func (e operationExecutor) ScanPage(ctx context.Context, job operations.Snapshot, input operations.ScanSpec, token string) (operations.ScanPage, error) {
	s, ctx, err := e.session(ctx, job)
	if err != nil {
		return operations.ScanPage{}, err
	}
	if err = e.app.AuthorizeOperation(ctx, s, job.Spec, false); err != nil {
		return operations.ScanPage{}, err
	}
	page, err := search.RecursivePage(ctx, NewClients(s), input.Bucket, input.Prefix, token)
	if err != nil {
		return operations.ScanPage{}, err
	}
	if err = search.NewListingGuard(input.Prefix, operations.MaxScanPages).Check(ctx, token, page); err != nil {
		return operations.ScanPage{}, err
	}
	result := operations.ScanPage{Objects: int64(len(page.Contents))}
	for _, object := range page.Contents {
		if object.Size > math.MaxInt64-result.Bytes {
			return operations.ScanPage{}, operations.ErrInvalid
		}
		result.Bytes += object.Size
	}
	if page.IsTruncated {
		result.NextToken = page.NextContinuationToken
	}
	return result, nil
}

type operationAdmission struct{ app *State }

func (a operationAdmission) Acquire(ctx context.Context, scope operations.Scope) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return a.app.AcquireTransfer(scope.ActorID)
}

func (app *State) RunOperations(ctx context.Context) {
	operations.Worker{Repository: app.Operations, Executor: operationExecutor{app}, Admission: operationAdmission{app}, Concurrency: 2,
		Monitor: app.operationMonitor,
		Report:  func(code string) { slog.Warn("background operation worker", "code", code) },
	}.Run(ctx)
}
