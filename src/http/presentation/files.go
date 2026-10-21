package presentation

import (
	"minio-web/api/src/services/files"
	"minio-web/api/src/services/localization"
	"minio-web/api/src/services/search"
)

type ContentSearchResult struct {
	files.ContentSearchResult
	Issues []ContentIssue `json:"issues"`
}

func presentFilesContentSearchResult(localizer *localization.Localizer, v files.ContentSearchResult) ContentSearchResult {
	return ContentSearchResult{ContentSearchResult: v,
		Issues: mapSlice(v.Issues, func(x search.ContentIssue) ContentIssue { return presentSearchContentIssue(localizer, x) }),
	}
}

type VersionProtection struct {
	files.VersionProtection
	Retention DetailField `json:"retention"`
	Hold      DetailField `json:"hold"`
}

func presentFilesVersionProtection(localizer *localization.Localizer, v files.VersionProtection) VersionProtection {
	return VersionProtection{VersionProtection: v,
		Retention: presentBucketsDetailField(localizer, v.Retention),
		Hold:      presentBucketsDetailField(localizer, v.Hold),
	}
}
