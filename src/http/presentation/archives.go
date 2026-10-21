package presentation

import (
	"github.com/owiks/golang-minio-s3-backend-api/src/services/archives"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/archives/plan"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/localization"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

type Manifest struct {
	archives.Manifest
	Files  []ArchiveItem     `json:"files"`
	Issues map[string]string `json:"issues"`
}

func presentArchivesManifest(localizer *localization.Localizer, v archives.Manifest) Manifest {
	return Manifest{Manifest: v,
		Files: mapSlice(v.Files, func(x archiveplan.ArchiveItem) ArchiveItem {
			return presentArchiveplanArchiveItem(localizer, x)
		}),
		Issues: mapValues(v.Issues, func(x messages.Ref) string { return localizer.Render(x) }),
	}
}

type ZIPContents struct {
	archives.ZIPContents
	Items []ZipItem `json:"items"`
}

func presentArchivesZIPContents(localizer *localization.Localizer, v archives.ZIPContents) ZIPContents {
	return ZIPContents{ZIPContents: v,
		Items: mapSlice(v.Items, func(x archives.ZipItem) ZipItem { return presentArchivesZipItem(localizer, x) }),
	}
}

type ZipItem struct {
	archives.ZipItem
	Reason string `json:"reason,omitempty"`
}

func presentArchivesZipItem(localizer *localization.Localizer, v archives.ZipItem) ZipItem {
	return ZipItem{ZipItem: v,
		Reason: localizer.Render(v.Reason),
	}
}
