package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/commandlog"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/operations"
)

// SubmitOperation is additive: callers choose a persisted job instead of the
// existing synchronous transfer. A replay resolves the current job, not a stale
// cached HTTP response, and is reauthorized against current grants.
func (app *State) SubmitOperation(ctx context.Context, s *Session, spec operations.Spec, key string) (job operations.Snapshot, commandID string, replayed bool, err error) {
	if err = app.AuthorizeOperation(ctx, s, spec, true); err != nil {
		return
	}
	scope := operations.Scope{ActorID: s.SessionID(), ConnectionID: s.ConnectionID()}
	if key == "" {
		job, err = app.Operations.Insert(ctx, scope, spec, s.ExpiresAt(), time.Now())
		err = operations.PublicError(err)
		return
	}
	if len(key) > 128 {
		err = commandlog.PublicError(commandlog.ErrInvalid)
		return
	}
	for _, c := range []byte(key) {
		if c < 33 || c > 126 {
			err = commandlog.PublicError(commandlog.ErrInvalid)
			return
		}
	}
	operation := "operations/transfers"
	if spec.Scan != nil {
		operation = "operations/scans"
	}
	commandScope := commandlog.Scope{ActorID: scope.ActorID, ConnectionID: scope.ConnectionID, Operation: operation}
	raw, _ := json.Marshal(spec)
	fingerprint, keyHash := sha256.Sum256(raw), sha256.Sum256([]byte(key))
	entry, fresh, reserveErr := app.Commands.Reserve(ctx, commandScope, hex.EncodeToString(keyHash[:]), hex.EncodeToString(fingerprint[:]), time.Now())
	commandID = entry.ID
	if reserveErr != nil {
		err = commandlog.PublicError(reserveErr)
		return
	}
	if !fresh {
		var saved struct {
			ID string `json:"id"`
		}
		if entry.Response == nil || json.Unmarshal(entry.Response.Payload, &saved) != nil || saved.ID == "" {
			err = commandlog.PublicError(commandlog.ErrUnavailable)
			return
		}
		job, err = app.Operations.Get(ctx, scope, saved.ID)
		if err != nil {
			err = operations.PublicError(err)
			return
		}
		if err = app.AuthorizeOperation(ctx, s, job.Spec, false); err != nil {
			return
		}
		return job, commandID, true, nil
	}
	finished := false
	defer func() {
		if !finished {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			_ = app.Commands.Finish(cleanup, commandID, commandScope, nil)
		}
	}()
	job, err = app.Operations.Insert(ctx, scope, spec, s.ExpiresAt(), time.Now())
	if err != nil {
		err = operations.PublicError(err)
		return
	}
	payload, _ := json.Marshal(struct {
		ID string `json:"id"`
	}{ID: job.ID})
	completion, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err = app.Commands.Finish(completion, commandID, commandScope, &commandlog.Response{Status: 202, Payload: payload}); err != nil {
		err = commandlog.PublicError(err)
		return
	}
	finished = true
	return
}
