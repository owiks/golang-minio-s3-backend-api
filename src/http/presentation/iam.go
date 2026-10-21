package presentation

import (
	"github.com/owiks/golang-minio-s3-backend-api/src/services/iam"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/localization"
)

type ActionCheckIssue struct {
	iam.ActionCheckIssue
	Message string `json:"message"`
}

func presentIamActionCheckIssue(localizer *localization.Localizer, v iam.ActionCheckIssue) ActionCheckIssue {
	return ActionCheckIssue{ActionCheckIssue: v,
		Message: localizer.Render(v.Message),
	}
}
