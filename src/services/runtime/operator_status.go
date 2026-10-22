package runtime

import (
	"context"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/operations"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/operatoraccess"
)

func (app *State) AuthorizeOperator(authorization []string) error {
	if !operatoraccess.ValidToken(app.config.OperatorToken) {
		return apperror.New(404, "NotFound", messages.RouteNotFound)
	}
	if !operatoraccess.Match(app.config.OperatorToken, authorization) {
		return apperror.New(401, "InvalidOperatorToken", messages.OperatorAuthentication)
	}
	return nil
}

func (app *State) OperatorStatus(ctx context.Context) (operations.OperatorStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	now := time.Now()
	summary, err := app.Operations.Summary(ctx, now)
	if err != nil {
		return operations.OperatorStatus{}, operations.PublicError(err)
	}
	p := app.config.Operations
	return operations.OperatorStatus{CheckedAt: now.UTC(), Queue: summary, Worker: app.operationMonitor.Snapshot(), Policy: operations.PolicyStatus{
		ReadOnly: p.ReadOnly, DisableUploads: p.DisableUploads, DisableDeletes: p.DisableDeletes, DisableAdminChanges: p.DisableAdminChanges,
	}}, nil
}
