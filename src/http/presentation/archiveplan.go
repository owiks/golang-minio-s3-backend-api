package presentation

import (
	"minio-web/api/src/services/archives/plan"
	"minio-web/api/src/services/localization"
	"minio-web/api/src/services/messages"
)

type ArchiveItem struct {
	archiveplan.ArchiveItem
	Message string `json:"message,omitempty"`
}

func presentArchiveplanArchiveItem(localizer *localization.Localizer, v archiveplan.ArchiveItem) ArchiveItem {
	return ArchiveItem{ArchiveItem: v,
		Message: localizer.Render(v.Message),
	}
}

type ArchiveSnapshot struct {
	archiveplan.ArchiveSnapshot
	Items  []ArchiveItem     `json:"items"`
	Issues map[string]string `json:"issues"`
}

func presentArchiveplanArchiveSnapshot(localizer *localization.Localizer, v archiveplan.ArchiveSnapshot) ArchiveSnapshot {
	return ArchiveSnapshot{ArchiveSnapshot: v,
		Items: mapSlice(v.Items, func(x archiveplan.ArchiveItem) ArchiveItem {
			return presentArchiveplanArchiveItem(localizer, x)
		}),
		Issues: mapValues(v.Issues, func(x messages.Ref) string { return localizer.Render(x) }),
	}
}
