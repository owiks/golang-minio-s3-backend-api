package users

import (
	"net/http"
	"strings"

	"minio-web/api/src/http/api/common"
	"minio-web/api/src/http/api/routes"
	"minio-web/api/src/http/api/routes/users/controllers"
	httpvalidation "minio-web/api/src/http/validation"
	"minio-web/api/src/services/identities"
	"minio-web/api/src/services/runtime"
)

func GetRoutes() []routes.APIRoute {
	return []routes.APIRoute{
		{
			Path: "admin/users",
			Tag:  "users",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Пользователи",
					Input:      routes.Inputs(),
					Controller: "HandleListUsers",
					Response:   []identities.UserDetails{}, Handler: func(app *runtime.
							State,
						w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleListUsers(app, w, r, s)
					},
				},
				{Suffix: "", Method: "POST", Mutation: routes.IAMLock, InvalidatesAccess: true, Summary: "Пользователи",
					Input:      routes.Inputs().Describe("acknowledgeNonAtomicCreate=true подтверждает риск внешней гонки: MinIO не поддерживает атомарное создание только отсутствующего пользователя. Уже найденный пользователь не изменяется; локальная блокировка не защищает от внешних клиентов."),
					Controller: "HandleCreateUser",
					Request:    identities.CreateUserRequest{},
					Response:   common.Success{},
					Required:   []string{"name", "secretKey", "acknowledgeNonAtomicCreate"}, Handler: func(app *runtime.
							State,
						w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleCreateUser(app, w, r, s)
					},
				},
				{Suffix: "{name}", Method: "GET", Summary: "Пользователи",
					Input:      routes.Inputs(),
					Controller: "HandleGetUser",
					Response:   identities.UserDetails{}, Handler: func(app *runtime.
							State,
						w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleGetUser(app, w, r, s, strings.SplitN(rest, "/", 2)[0])
					},
				},
				{Suffix: "{name}", Method: "DELETE",
					Request: httpvalidation.ConfirmationRequest{}, Mutation: routes.IAMLock, InvalidatesAccess: true, Summary: "Пользователи",
					Input:      routes.Inputs(),
					Controller: "HandleDeleteUser",
					Response:   common.Success{}, Handler: func(app *runtime.
							State,
						w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleDeleteUser(app, w, r, s, strings.SplitN(rest, "/", 2)[0])
					},
				},
				{Suffix: "{name}/status", Method: "PUT", Mutation: routes.IAMLock, InvalidatesAccess: true, Summary: "Пользователи",
					Input:      routes.Inputs(),
					Controller: "HandleSetUserStatus",
					Request:    identities.StatusRequest{},
					Response:   common.Success{}, Handler: func(app *runtime.
							State,
						w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleSetUserStatus(app, w, r, s, strings.SplitN(rest, "/", 2)[0])
					},
				},
			},
		},
	}
}
