package access

import (
	"net/http"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/access/controllers"
	accessdto "github.com/owiks/golang-minio-s3-backend-api/src/services/access"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

func GetRoutes() []routes.APIRoute {
	return []routes.APIRoute{
		{
			Path: "access",
			Tag:  "access",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Доступные разделы и бакеты",
					Input:      routes.Inputs(),
					Controller: "HandleAccess",
					Response:   accessdto.AccessSnapshot{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleAccess(app, w, r, s)
					}},
			},
		},
		{
			Path: "access-version",
			Tag:  "access",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Ревизия прав доступа",
					Input:      routes.Inputs(),
					Controller: "HandleAccessVersion",
					Response:   accessdto.AccessRevision{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleAccessVersion(app, w, r, s)
					}},
			},
		},
		{
			Path: "action-access",
			Tag:  "access",
			Operations: []routes.Operation{
				{Suffix: "", Method: "POST", Mutation: routes.NoLock, Summary: "Проверка прав на действия",
					Input:      routes.Inputs(),
					Request:    controllers.ActionAccessRequest{},
					Required:   []string{"queries"},
					Controller: "HandleActionAccess",
					Response:   accessdto.ActionAccess{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleActionAccess(app, w, r, s)
					}},
			},
		},
	}
}
