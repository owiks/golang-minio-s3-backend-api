package presentation

import (
	"minio-web/api/src/services/access"
	"minio-web/api/src/services/iam"
	"minio-web/api/src/services/localization"
)

type AccessSnapshot struct {
	access.AccessSnapshot
	Access NavigationAccess `json:"access"`
}

func presentAccessAccessSnapshot(localizer *localization.Localizer, v access.AccessSnapshot) AccessSnapshot {
	return AccessSnapshot{AccessSnapshot: v,
		Access: presentAccessNavigationAccess(localizer, v.Access),
	}
}

type ActionAccess struct {
	access.ActionAccess
	Issue *ActionCheckIssue `json:"issue"`
}

func presentAccessActionAccess(localizer *localization.Localizer, v access.ActionAccess) ActionAccess {
	return ActionAccess{ActionAccess: v,
		Issue: mapPointer(v.Issue, func(x iam.ActionCheckIssue) ActionCheckIssue { return presentIamActionCheckIssue(localizer, x) }),
	}
}

type NavigationAccess struct {
	access.NavigationAccess
	ActionCheck *ActionCheckIssue `json:"actionCheck,omitempty"`
}

func presentAccessNavigationAccess(localizer *localization.Localizer, v access.NavigationAccess) NavigationAccess {
	return NavigationAccess{NavigationAccess: v,
		ActionCheck: mapPointer(v.ActionCheck, func(x iam.ActionCheckIssue) ActionCheckIssue { return presentIamActionCheckIssue(localizer, x) }),
	}
}
