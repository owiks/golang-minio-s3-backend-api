package transfers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/commandlog"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

type commandResult struct {
	Copied bool `json:"copied"`
	Moved  bool `json:"moved"`
}

// TransferCommand only records this explicitly supported DTO, never an HTTP
// response, presented credential or a localized message. Without a key the
// original synchronous behavior is preserved.
func TransferCommand(ctx context.Context, repo commandlog.Repository, client TransferClient, scope commandlog.Scope, key string, body FileTransferRequest) (result TransferResult, id string, replayed bool, err error) {
	if key == "" {
		result, err = Transfer(ctx, client, body)
		return
	}
	if len(key) > 128 {
		return result, "", false, commandlog.PublicError(commandlog.ErrInvalid)
	}
	for _, c := range []byte(key) {
		if c < 33 || c > 126 {
			return result, "", false, commandlog.PublicError(commandlog.ErrInvalid)
		}
	}
	// Replays cannot disclose an earlier result after a grant was narrowed.
	if err = AuthorizeTransfer(ctx, client, body); err != nil {
		return
	}
	input, _ := json.Marshal(body)
	fingerprint, keyHash := sha256.Sum256(input), sha256.Sum256([]byte(key))
	entry, fresh, reserveErr := repo.Reserve(ctx, scope, hex.EncodeToString(keyHash[:]), hex.EncodeToString(fingerprint[:]), time.Now())
	id = entry.ID
	if reserveErr != nil {
		err = commandlog.PublicError(reserveErr)
		return
	}
	if !fresh {
		var saved commandResult
		if entry.Response == nil || entry.Response.Status != 200 || json.Unmarshal(entry.Response.Payload, &saved) != nil || !knownMessage(entry.Response.MessageID) {
			err = commandlog.PublicError(commandlog.ErrUnavailable)
			return
		}
		return TransferResult{Copied: saved.Copied, Moved: saved.Moved, Code: entry.Response.Code, Message: messages.ID(entry.Response.MessageID)}, id, true, nil
	}
	finished := false
	defer func() {
		if !finished {
			// Cancellation and a panic must not authorize a second execution.
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			_ = repo.Finish(cleanup, id, scope, nil)
		}
	}()
	result, err = Transfer(ctx, client, body)
	if err != nil {
		return
	}
	description := messages.Describe(result.Message)
	if description.IsLiteral || len(description.Arguments) != 0 || !knownMessage(string(description.ID)) {
		err = commandlog.PublicError(commandlog.ErrUnavailable)
		return
	}
	payload, _ := json.Marshal(commandResult{Copied: result.Copied, Moved: result.Moved})
	response := commandlog.Response{Status: 200, Payload: payload, Code: result.Code, MessageID: string(description.ID)}
	completion, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if finishErr := repo.Finish(completion, id, scope, &response); finishErr != nil {
		err = commandlog.PublicError(commandlog.ErrUnavailable)
		return
	}
	finished = true
	return
}

func knownMessage(value string) bool {
	if value == "" {
		return false
	}
	for _, id := range messages.AllIDs() {
		if string(id) == value {
			return true
		}
	}
	return false
}
