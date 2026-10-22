package runtime

import (
	"context"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/commandlog"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/operationpolicy"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/transfers"
)

// ExecuteTransferCommand composes operator restrictions and durable execution.
// The HTTP adapter only decodes input and projects the result.
func (app *State) ExecuteTransferCommand(ctx context.Context, s *Session, input transfers.FileTransferRequest, key string) (transfers.TransferResult, string, bool, error) {
	action := operationpolicy.Upload
	if input.Move {
		action |= operationpolicy.Delete
	}
	if err := app.config.Operations.Check(action); err != nil {
		return transfers.TransferResult{}, "", false, err
	}
	return transfers.TransferCommand(ctx, app.Commands, NewClients(s), commandlog.Scope{ActorID: s.SessionID(), ConnectionID: s.ConnectionID(), Operation: "objects/transfer"}, key, input)
}
