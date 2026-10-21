package keys

import (
	"net/http"
	"strings"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/keys/controllers"
	httpvalidation "github.com/owiks/golang-minio-s3-backend-api/src/http/validation"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/keys"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

func GetRoutes() []routes.APIRoute {
	return []routes.APIRoute{
		{
			Path: "admin/access-keys",
			Tag:  "keys",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Ключи доступа",
					Input:      routes.Inputs(routes.EnumQuery("scope", false, "own", "user", "all"), routes.Query("parent", false, 128).Describe("Обязателен при scope=user.")).Describe("scope по умолчанию own. parent разрешён и обязателен только при scope=user."),
					Controller: "HandleListKeys",
					Response:   []keys.AccessKeyRow{}, Handler: func(app *runtime.
							State, w http.ResponseWriter,

						r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleListKeys(app, w, r, s, false)
					},
				},
				{Suffix: "", Method: "POST", Mutation: routes.IAMLock, InvalidatesAccess: true, Summary: "Ключи доступа",
					Input:      routes.Inputs().Describe("policy=null означает наследование прав пользователя."),
					Controller: "HandleCreateKey",
					Request:    keys.KeyInput{},
					Response:   keys.GeneratedKey{}, Handler: func(app *runtime.
							State, w http.ResponseWriter,

						r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleCreateKey(app, w, r, s)
					},
				},
				{Suffix: "{name}", Method: "GET", Summary: "Ключи доступа",
					Input:      routes.Inputs().Describe("scope по умолчанию own. parent разрешён и обязателен только при scope=user."),
					Controller: "HandleGetKey",
					Response:   keys.AccessKeyDetails{}, Handler: func(app *runtime.
							State, w http.ResponseWriter,

						r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleGetKey(app, w, r, s, rest, false)
					},
				},
				{Suffix: "{name}", Method: "PUT", Mutation: routes.IAMLock, InvalidatesAccess: true, Summary: "Ключи доступа",
					Input:      routes.Inputs().Describe("Передайте policy (null — наследование) и актуальную revision."),
					Controller: "HandleEditKey",
					Request:    keys.KeyInput{},
					Response:   common.Success{},
					Required:   []string{"policy", "revision"}, Handler: func(app *runtime.
							State, w http.ResponseWriter,

						r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleEditKey(app, w, r, s, strings.SplitN(rest, "/", 2)[0])
					},
				},
				{Suffix: "{name}", Method: "DELETE",
					Request: httpvalidation.ConfirmationRequest{}, Mutation: routes.IAMLock, InvalidatesAccess: true, Summary: "Ключи доступа",
					Input:      routes.Inputs().Describe("confirmation должен совпадать с Access Key."),
					Controller: "HandleDeleteKey",
					Response:   common.Success{}, Handler: func(app *runtime.
							State, w http.ResponseWriter,

						r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleDeleteKey(app, w, r, s, strings.SplitN(rest, "/", 2)[0])
					},
				},
				{Suffix: "{name}/status", Method: "PUT", Mutation: routes.IAMLock, InvalidatesAccess: true, Summary: "Ключи доступа",
					Input:      routes.Inputs().Describe("Передайте policy (null — наследование) и актуальную revision."),
					Controller: "HandleSetKeyStatus",
					Request:    keys.StatusRequest{},
					Response:   common.Success{}, Handler: func(app *runtime.
							State, w http.ResponseWriter,

						r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleSetKeyStatus(app, w, r, s, strings.SplitN(rest, "/", 2)[0])
					},
				},
				{Suffix: "{name}/rotate", Method: "POST", Mutation: routes.IAMLock, InvalidatesAccess: true, Summary: "Ключи доступа",
					Input:      routes.Inputs().Describe("policy=null означает наследование прав пользователя."),
					Controller: "HandleRotateKeySecret",
					Request:    keys.RotateRequest{},
					Response:   keys.GeneratedKey{}, Handler: func(app *runtime.
							State, w http.ResponseWriter,

						r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleRotateKeySecret(app, w, r, s, strings.SplitN(rest, "/", 2)[0])
					},
				},
			},
		},
		{
			Path: "admin/temporary-access",
			Tag:  "keys",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Временные доступы",
					Input:      routes.Inputs(routes.EnumQuery("scope", false, "own", "user", "all"), routes.Query("parent", false, 128).Describe("Обязателен при scope=user.")),
					Controller: "HandleListKeys",
					Response:   []keys.AccessKeyRow{}, Handler: func(app *runtime.
							State, w http.ResponseWriter,

						r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleListKeys(app, w, r, s, true)
					},
				},
				{Suffix: "{name}", Method: "GET", Summary: "Временные доступы",
					Input:      routes.Inputs(),
					Controller: "HandleGetKey",
					Response:   keys.AccessKeyDetails{}, Handler: func(app *runtime.
							State, w http.ResponseWriter,

						r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleGetKey(app, w, r, s, rest, true)
					},
				},
			},
		},
	}
}
