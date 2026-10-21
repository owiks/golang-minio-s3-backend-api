package presentation

import (
	"minio-web/api/src/services/localization"
	"minio-web/api/src/services/search"
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
