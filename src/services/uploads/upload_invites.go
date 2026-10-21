package uploads

import (
	"context"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/iam"
	"minio-web/api/src/services/messages"
)

func EvaluateInviteScope(ctx context.Context, s EvaluateInviteScopeClient, bucket, prefix string) (bool, error) {
	if issue := s.RefreshActionPolicy(ctx); issue != nil {
		return false, apperror.New(403, "InvitePermissionsUnavailable", messages.UploadsInvitePermissionsUnavailable)
	}
	policy := s.ActionPolicySnapshot()
	return policy.Allows(s.ActorName(), iam.Query{Action: "s3:PutObject", Bucket: bucket, Key: prefix + "*"}), nil
}
