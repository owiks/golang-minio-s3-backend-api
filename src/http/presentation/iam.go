package presentation

import (
	"minio-web/api/src/services/iam"
	"minio-web/api/src/services/localization"
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
