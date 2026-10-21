package bindings

import (
	"net/http"

	"minio-web/api/src/http/api/common"
	"minio-web/api/src/http/api/routes"
	"minio-web/api/src/http/api/routes/bindings/controllers"
	"minio-web/api/src/services/identities"
	"minio-web/api/src/services/runtime"
)

func GetRoutes() []routes.APIRoute {
	return []routes.APIRoute{
		{
			Path: "admin/bindings",
			Tag:  "bindings",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Назначения политик",
					Input:      routes.Inputs(routes.Query("group", false, 128), routes.Query("policy", false, 128), routes.Query("user", false, 128)).Describe("Передайте ровно один фильтр: user, group или policy."),
					Controller: "HandleGetBindings",
					Response:   identities.Bindings{}, Handler: func(app *runtime.
							State, w http.
							ResponseWriter, r *http.
							Request, s *runtime.Session, rest string) error {
						return controllers.HandleGetBindings(app, w, r, s)
					},
				},
				{Suffix: "", Method: "POST", Mutation: routes.IAMLock, InvalidatesAccess: true, Summary: "Назначения политик",
					Input:      routes.Inputs().Describe("Укажите policy и ровно одно поле: user или group. remove=true отвязывает политику."),
					Controller: "HandleSaveBinding",
					Request:    identities.BindingRequest{},
					Response:   common.Success{}, Handler: func(app *runtime.
							State, w http.
							ResponseWriter, r *http.
							Request, s *runtime.Session, rest string) error {
						return controllers.HandleSaveBinding(app, w, r, s)
					},
				},
			},
		},
	}
}
