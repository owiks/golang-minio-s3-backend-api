package presentation

import (
	"github.com/owiks/golang-minio-s3-backend-api/src/services/localization"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/sessions"
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
