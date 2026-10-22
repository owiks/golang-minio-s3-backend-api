package transfers

import (
	"context"
	"strings"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	folderssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/folders"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	objectssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/objects"
	objectstate "github.com/owiks/golang-minio-s3-backend-api/src/services/objects/state"
	objectwrite "github.com/owiks/golang-minio-s3-backend-api/src/services/objects/write"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/telemetry"
)

type FileTransferRequest struct {
	SourceBucket        string `json:"sourceBucket"`
	SourceKey           string `json:"sourceKey"`
	SourceRevision      string `json:"sourceRevision"`
	SourcePrefix        string `json:"sourcePrefix"`
	DestinationPrefix   string `json:"destinationPrefix"`
	DestinationBucket   string `json:"destinationBucket"`
	DestinationKey      string `json:"destinationKey"`
	DestinationRevision string `json:"destinationRevision"`
	Move                bool   `json:"move"`
}

type TransferResult struct {
	Copied  bool         `json:"copied"`
	Moved   bool         `json:"moved"`
	Message messages.Ref `json:"message"`
	Code    string       `json:"code,omitempty"`
}

// AuthorizeTransfer checks the mapping and current grants before execution or replay.
func AuthorizeTransfer(ctx context.Context, s interface {
	AuthorizeResource(context.Context, string, string, string) error
}, body FileTransferRequest) error {
	if body.SourceBucket == "" || body.DestinationBucket == "" || !ValidTransferMapping(body.SourceKey, body.DestinationKey, body.SourcePrefix, body.DestinationPrefix, body.SourceBucket == body.DestinationBucket) {
		return apperror.New(400, "InvalidTransfer", messages.TransfersInvalidTransfer)
	}
	if body.SourceBucket == body.DestinationBucket && body.SourceKey == body.DestinationKey {
		return apperror.New(400, "SameDestination", messages.TransfersSameDestination)
	}
	if err := s.AuthorizeResource(ctx, "objects:read", body.SourceBucket, body.SourceKey); err != nil {
		return err
	}
	if err := s.AuthorizeResource(ctx, "objects:write", body.DestinationBucket, body.DestinationKey); err != nil {
		return err
	}
	if body.Move {
		if err := s.AuthorizeResource(ctx, "objects:delete", body.SourceBucket, body.SourceKey); err != nil {
			return err
		}
	}

	return nil
}

func Transfer(ctx context.Context, s TransferClient, body FileTransferRequest) (result TransferResult, err error) {
	defer func() {
		if err != nil {
			telemetry.Outcome(ctx, "error", apperror.Public(err).Code)
		} else if body.Move && !result.Moved {
			telemetry.Outcome(ctx, "partial", result.Code)
		} else {
			telemetry.Outcome(ctx, "success", "")
		}
	}()

	if err = AuthorizeTransfer(ctx, s, body); err != nil {
		return TransferResult{}, err
	}

	source, err := s.StatObject(ctx, body.SourceBucket, body.SourceKey, minio.StatObjectOptions{})
	if err != nil {
		return TransferResult{}, err
	}
	snapshot := objectstate.DeletionSnapshot{ETag: source.ETag, Size: source.Size, Modified: source.LastModified, Version: source.VersionID}
	if body.SourceRevision == "" || body.SourceRevision != objectstate.DeletionRevision(snapshot) {
		return TransferResult{}, apperror.New(409, "SourceChanged", messages.TransfersSourceChanged)
	}
	if source.Size > objectwrite.MaxFileSize {
		return TransferResult{}, apperror.New(400, "TransferTooLarge", messages.TransfersTransferTooLarge)
	}
	target, err := objectwrite.DestinationState(ctx, s, body.DestinationBucket, body.DestinationKey)
	if err != nil {
		return TransferResult{}, err
	}
	opts, err := objectwrite.WriteOptions(target, body.DestinationRevision, source.ContentType)
	if err != nil {
		return TransferResult{}, err
	}
	get := minio.GetObjectOptions{VersionID: source.VersionID}
	_ = get.SetMatchETag(source.ETag)
	tagset, err := s.GetObjectTagging(ctx, body.SourceBucket, body.SourceKey, minio.GetObjectTaggingOptions{VersionID: source.VersionID})
	if err != nil {
		return TransferResult{}, err
	}
	opts.UserTags = tagset.ToMap()
	opts.UserMetadata = source.UserMetadata
	opts.ContentEncoding = source.Metadata.Get("Content-Encoding")
	opts.ContentDisposition = source.Metadata.Get("Content-Disposition")
	opts.ContentLanguage = source.Metadata.Get("Content-Language")
	opts.CacheControl = source.Metadata.Get("Cache-Control")
	opts.Expires = source.Expires
	copied, handled, err := copyOnServer(ctx, s, body, source, opts)
	if !handled {
		object, readErr := s.GetObject(ctx, body.SourceBucket, body.SourceKey, get)
		if readErr != nil {
			return TransferResult{}, readErr
		}
		defer object.Close()
		copied, err = s.PutObject(ctx, body.DestinationBucket, body.DestinationKey, object, source.Size, opts)
		telemetry.Event(ctx, "copy uses streaming fallback")
	} else {
		telemetry.Event(ctx, "copy uses server parts")
	}
	if err != nil {
		return TransferResult{}, objectwrite.FileWriteError(err)
	}

	if !body.Move {
		return TransferResult{Copied: true, Moved: false, Message: messages.TransfersTheCopyWasCreated}, nil
	}
	// A marker is removed only after its children; listing happens before the
	// final snapshots so it cannot lengthen the check-to-delete window.
	if source.Size == 0 && strings.HasSuffix(body.SourceKey, "/") {
		children, checkErr := folderssvc.FolderMarkerHasChildren(ctx, s, body.SourceBucket, body.SourceKey)
		if checkErr != nil || children {
			return TransferResult{Copied: true, Code: "FolderNotEmpty", Message: messages.TransfersTheFolderMarkerWasCopiedAndThe}, nil
		}
	}
	destination, destErr := s.StatObject(ctx, body.DestinationBucket, body.DestinationKey, minio.StatObjectOptions{})
	if destErr != nil {
		return TransferResult{Copied: true, Code: "TransferVerificationFailed", Message: messages.TransfersTheCopyWasCreatedButItsState}, nil
	}
	if destination.ETag != copied.ETag || destination.VersionID != copied.VersionID || destination.Size != source.Size {
		return TransferResult{Copied: true, Code: "DestinationChanged", Message: messages.TransfersTheDestinationChangedAfterCopyingTheSource}, nil
	}
	current, readErr := objectssvc.ReadDeletion(ctx, s, body.SourceBucket, body.SourceKey)
	if readErr != nil {
		return TransferResult{Copied: true, Code: "TransferVerificationFailed", Message: messages.TransfersTheCopyWasCreatedButTheSource}, nil
	}
	if objectstate.DeletionRevision(current) != body.SourceRevision {
		return TransferResult{Copied: true, Code: "SourceChanged", Message: messages.TransfersTheSourceFileChangedAfterCopyingAnd}, nil
	}
	if s.ConditionalDeleteSupported() {
		err = s.RemoveConditional(ctx, body.SourceBucket, body.SourceKey, source.ETag)
	} else {
		// MinIO releases that ignore DELETE If-Match retain their ordinary S3
		// move semantics: checked copy, final snapshots, ordinary DELETE. Never
		// target a version or bypass retention. This is not a two-key transaction.
		telemetry.Event(ctx, "move uses checked ordinary source deletion")
		err = s.RemoveObject(ctx, body.SourceBucket, body.SourceKey, minio.RemoveObjectOptions{})
		if err != nil {
			switch minio.ToErrorResponse(err).Code {
			case "AccessDenied", "InvalidRequest", "ObjectLocked":
				err = apperror.New(403, "ObjectDeleteDenied", messages.TransfersObjectDeleteDenied)
			default:
				err = apperror.New(502, "DeleteUncertain", messages.TransfersDeleteUncertain)
			}
		}
	}
	if err != nil {
		issue := apperror.Public(err)
		return TransferResult{Copied: true, Message: issue.Message, Code: issue.Code}, nil
	}
	// A successful DELETE alone must not claim a completed move if the server
	// retained the source, a writer recreated it, or verification was denied.
	_, verifyErr := s.StatObject(ctx, body.SourceBucket, body.SourceKey, minio.StatObjectOptions{})
	if minio.ToErrorResponse(verifyErr).Code == "NoSuchKey" {
		return TransferResult{Copied: true, Moved: true, Message: messages.TransfersTheCopyWasCreatedAndSourceDeletion}, nil
	}
	if verifyErr == nil {
		return TransferResult{Copied: true, Code: "SourceStillExists", Message: messages.TransfersTheCopyWasCreatedButTheSourceFileTransfer}, nil
	}
	return TransferResult{Copied: true, Code: "DeleteUncertain", Message: messages.TransfersTheCopyWasCreatedButTheSourceFileTransferTransfer}, nil
}
