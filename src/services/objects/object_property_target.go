package objects

import (
	"context"
	"errors"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
)

// A concrete version fixes object identity, not its mutable tags. The null
// version is itself a mutable slot when versioning is absent or suspended.
func immutablePropertyVersion(version string) bool { return version != "" && version != "null" }

type propertyTargetReader interface {
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
}

// A mutable target has no conditional tagging API. Fence the snapshot before
// writing and verify afterwards; these checks do not make the write atomic.
func verifyPropertyTarget(ctx context.Context, s propertyTargetReader, bucket, key string, expected minio.ObjectInfo) error {
	current, err := s.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil {
		switch minio.ToErrorResponse(err).Code {
		case "NoSuchKey", "NoSuchVersion":
			return propertyTargetChanged()
		}
		return err
	}
	if current.ETag != expected.ETag || current.Size != expected.Size || !current.LastModified.Equal(expected.LastModified) || current.VersionID != expected.VersionID {
		return propertyTargetChanged()
	}
	return nil
}

func propertyTargetChanged() error {
	return apperror.New(409, "ObjectPropertiesChanged", messages.ObjectsObjectPropertiesChangedObjectPropertyTarget)
}

func propertyContentReadError(err error) error {
	switch minio.ToErrorResponse(err).Code {
	case "NoSuchVersion", "NoSuchKey", "PreconditionFailed", "ConditionalRequestConflict":
		return apperror.New(409, "ObjectChanged", messages.ObjectsObjectChangedObjectPropertyTarget)
	}
	return err
}

func verifyTaggedTarget(ctx context.Context, s propertyTargetReader, bucket, key string, expected minio.ObjectInfo) error {
	err := verifyPropertyTarget(ctx, s, bucket, key, expected)
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if apperror.Public(err).Code == "ObjectPropertiesChanged" {
		return apperror.New(409, "ObjectPropertiesChanged", messages.ObjectsObjectPropertiesChangedObjectPropertyTargetTaggedTarget)
	}
	return apperror.New(502, "ObjectTagsUnverified", messages.ObjectsObjectTagsUnverified)
}
