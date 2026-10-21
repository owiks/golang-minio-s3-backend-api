package presentation

import (
	"github.com/owiks/golang-minio-s3-backend-api/src/services/localization"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/search"
)

type ContentIssue struct {
	search.ContentIssue
	Message string `json:"message"`
}

func presentSearchContentIssue(localizer *localization.Localizer, v search.ContentIssue) ContentIssue {
	return ContentIssue{ContentIssue: v,
		Message: localizer.Render(v.Message),
	}
}
