package capabilities

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/s3utils"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
	"minio-web/api/src/services/storage"
)

type ProbeClient interface {
	ReadObjectAttributes(context.Context, string, string, storage.ObjectAttributesOptions) (storage.ObjectAttributesPage, error)
	ListMultipartUploads(context.Context, string, string, string, string, string, int) (minio.ListMultipartUploadsResult, error)
}

func Validate(request Request) error {
	s := request.Scope
	if request.Feature != "" && !Known(request.Feature) {
		return apperror.New(400, "InvalidCapability", messages.CapabilitiesInvalidCapability)
	}
	if s.Bucket != "" && s3utils.CheckValidBucketNameStrict(s.Bucket) != nil {
		return apperror.New(400, "InvalidBucket", messages.BucketsInvalidBucketName)
	}
	if len(s.Key) > 1024 || len(s.VersionID) > 1024 || !utf8.ValidString(s.Key) || !utf8.ValidString(s.VersionID) || strings.ContainsRune(s.Key, 0) || strings.ContainsRune(s.VersionID, 0) {
		return apperror.New(400, "InvalidCapabilityScope", messages.CapabilitiesInvalidCapabilityScope)
	}
	if s.Bucket == "" && (s.Key != "" || s.VersionID != "") || s.Key == "" && s.VersionID != "" {
		return apperror.New(400, "InvalidCapabilityScope", messages.CapabilitiesInvalidCapabilityScopeReportUsecase)
	}
	return nil
}

func Report(ctx context.Context, client ProbeClient, cache Cache, request Request) (Snapshot, error) {
	if err := Validate(request); err != nil {
		return Snapshot{}, err
	}
	ids := Identifiers()
	if request.Feature != "" {
		ids = []Identifier{request.Feature}
	}
	result := Snapshot{Capabilities: make([]Result, 0, len(ids))}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		load := func(ctx context.Context) (Result, error) { return probe(ctx, client, id, request.Scope) }
		var value Result
		var err error
		if cache != nil {
			value, err = cache.Resolve(ctx, id, request.Scope, load)
		} else {
			value, err = load(ctx)
		}
		if err != nil {
			return Snapshot{}, err
		}
		result.Capabilities = append(result.Capabilities, value)
	}
	return result, nil
}

func probe(ctx context.Context, client ProbeClient, id Identifier, scope Scope) (Result, error) {
	now := time.Now()
	if id == ConditionalComplete {
		if configured, ok := client.(interface{ ConditionalCompletionConfigured() bool }); ok && configured.ConditionalCompletionConfigured() {
			value := Unchecked(id, scope, "EndpointConfigured", now)
			value.Status, value.Evidence = Supported, "confirmed_configuration"
			value.ExpiresAt = value.CheckedAt.Add(TTL(Supported))
			return value, nil
		}
	}
	if scope.Bucket == "" {
		return Unchecked(id, scope, "BucketRequired", now), nil
	}
	if id != MultipartList && id != ObjectAttributes {
		// Discovering writes, checksum modes, browser CORS and Select by calling
		// them would mutate data or read arbitrary object contents.
		return Unchecked(id, scope, "RequiresObservedOperation", now), nil
	}
	if id == ObjectAttributes && scope.Key == "" {
		return Unchecked(id, scope, "ObjectRequired", now), nil
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var err error
	switch id {
	case ObjectAttributes:
		_, err = client.ReadObjectAttributes(bounded, scope.Bucket, scope.Key, storage.ObjectAttributesOptions{VersionID: scope.VersionID, MaxParts: 1})
	case MultipartList:
		_, err = client.ListMultipartUploads(bounded, scope.Bucket, "", "", "", "", 1)
	}
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	return Outcome(id, scope, "read_probe", err, time.Now()), nil
}
