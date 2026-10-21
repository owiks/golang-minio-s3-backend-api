package presentation

import (
	"minio-web/api/src/services/localization"
	"minio-web/api/src/services/sessions"
)

type LoginResponse struct {
	sessions.LoginResponse
	Access NavigationAccess `json:"access"`
}

func presentSessionsLoginResponse(localizer *localization.Localizer, v sessions.LoginResponse) LoginResponse {
	return LoginResponse{LoginResponse: v,
		Access: presentAccessNavigationAccess(localizer, v.Access),
	}
}
