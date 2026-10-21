package presentation

import (
	"github.com/owiks/golang-minio-s3-backend-api/src/services/localization"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/objects"
)

type ChecksumInfo struct {
	objects.ChecksumInfo
	Warning string `json:"warning,omitempty"`
}

func presentObjectsChecksumInfo(localizer *localization.Localizer, v objects.ChecksumInfo) ChecksumInfo {
	return ChecksumInfo{ChecksumInfo: v,
		Warning: localizer.Render(v.Warning),
	}
}

type Content struct {
	objects.Content
	Reason string `json:"reason"`
}

func presentObjectsContent(localizer *localization.Localizer, v objects.Content) Content {
	return Content{Content: v,
		Reason: localizer.Render(v.Reason),
	}
}

type ObjectProperties struct {
	objects.ObjectProperties
	TagsError      string `json:"tagsError,omitempty"`
	MetadataReason string `json:"metadataReason,omitempty"`
}

func presentObjectsObjectProperties(localizer *localization.Localizer, v objects.ObjectProperties) ObjectProperties {
	return ObjectProperties{ObjectProperties: v,
		TagsError:      localizer.Render(v.TagsError),
		MetadataReason: localizer.Render(v.MetadataReason),
	}
}
