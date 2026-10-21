package capabilities

import (
	"context"
	"errors"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
)

// Outcome publishes only a fixed reason code. SDK messages can contain paths,
// credentials or response bodies and must never become capability evidence.
func Outcome(id Identifier, scope Scope, evidence string, err error, now time.Time) Result {
	r := Result{ID: id, Scope: scope, Status: Unknown, Access: AccessUnknown, Evidence: evidence, Reason: "NotChecked", CheckedAt: now.UTC()}
	if err == nil {
		r.Status, r.Reason = Supported, "OperationSucceeded"
		r.Access = AccessAllowed
	} else {
		var own *apperror.Error
		var upstream minio.ErrorResponse
		var upstreamPointer *minio.ErrorResponse
		hasUpstream := errors.As(err, &upstream)
		if !hasUpstream && errors.As(err, &upstreamPointer) && upstreamPointer != nil {
			upstream, hasUpstream = *upstreamPointer, true
		}
		if errors.As(err, &own) {
			switch own.Code {
			case "ObjectAttributesUnsupported", "NotImplemented":
				if own.Status == 501 {
					r.Status, r.Reason = Unsupported, "OperationNotImplemented"
				} else {
					r.Reason = "UpstreamUncertain"
				}
			case "AccessDenied", "InvalidAccessKeyId", "SignatureDoesNotMatch", "ExpiredToken", "InvalidToken":
				r.Status, r.Reason = AccessDenied, "AccessDenied"
				r.Access = AccessForbidden
			case "NoSuchVersion":
				r.Reason = "ObjectNotFound"
			case "InvalidObjectAttributesResponse", "InvalidS3ListingResponse", "S3ResponseTooLarge", "InvalidSelectEvent", "SelectUpstreamLimit":
				r.Reason = "InvalidResponse"
			default:
				r.Reason = "UpstreamUncertain"
			}
		} else if hasUpstream {
			switch upstream.Code {
			case "AccessDenied", "AllAccessDisabled", "InvalidAccessKeyId", "SignatureDoesNotMatch", "ExpiredToken", "InvalidToken":
				r.Status, r.Reason = AccessDenied, "AccessDenied"
				r.Access = AccessForbidden
			case "NotImplemented", "XNotImplemented", "UnsupportedOperation":
				// A typed protocol error is required; generic HTTP 501 alone is not
				// sufficient evidence that this particular operation is absent.
				if upstream.StatusCode == 0 || upstream.StatusCode == 501 || upstream.StatusCode >= 400 && upstream.StatusCode < 500 {
					r.Status, r.Reason = Unsupported, "OperationNotImplemented"
				} else {
					r.Reason = "UpstreamUncertain"
				}
			case "NoSuchKey", "NoSuchVersion":
				r.Reason = "ObjectNotFound"
			case "NoSuchBucket":
				r.Reason = "BucketNotFound"
			default:
				r.Reason = "UpstreamUncertain"
			}
		} else if errors.Is(err, context.DeadlineExceeded) {
			r.Reason = "Timeout"
		} else if errors.Is(err, context.Canceled) {
			r.Reason = "Canceled"
		} else {
			r.Reason = "UpstreamUncertain"
		}
	}
	r.ExpiresAt = r.CheckedAt.Add(TTL(r.Status))
	return r
}

func Unchecked(id Identifier, scope Scope, reason string, now time.Time) Result {
	r := Result{ID: id, Scope: scope, Status: Unknown, Access: AccessUnknown, Evidence: "none", Reason: reason, CheckedAt: now.UTC()}
	r.ExpiresAt = r.CheckedAt.Add(TTL(r.Status))
	return r
}
