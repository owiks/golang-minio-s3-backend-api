package tiers

import (
	"net/http"

	"minio-web/api/src/http/api/routes"
	"minio-web/api/src/http/api/routes/tiers/controllers"
	resultdto "minio-web/api/src/services/result"
	"minio-web/api/src/services/runtime"
	tiersdto "minio-web/api/src/services/tiers"
)

func GetRoutes() []routes.APIRoute {
	return []routes.APIRoute{
		{
			Path: "admin/tiers",
			Tag:  "tiers",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Уровни хранения",
					Input:      routes.Inputs(),
					Controller: "HandleListTiers",
					Response:   []tiersdto.TierView{}, Handler: func(app *runtime.
							State,
						w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleListTiers(app, w, r, s)
					},
				},
				{Suffix: "", Method: "POST", Mutation: routes.AdminLock, Summary: "Уровни хранения",
					Input:      routes.Inputs().Describe("POST без operation создаёт уровень: нужны name, type (s3/minio), endpoint, bucket, accessKey и secretKey. POST с operation=verify проверяет существующий уровень и требует только name. PUT меняет ключи: нужны revision и confirmation с именем уровня. DELETE требует name, revision и confirmation."),
					Request:    tiersdto.TiersRequest{},
					Required:   []string{"name"},
					Controller: "HandleCreateOrVerifyTier",
					Response:   resultdto.Success{}, Handler: func(app *runtime.
							State,
						w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleCreateOrVerifyTier(app, w, r, s)
					},
				},
				{Suffix: "", Method: "PUT", Mutation: routes.AdminLock, Summary: "Уровни хранения",
					Input:      routes.Inputs().Describe("POST без operation создаёт уровень: нужны name, type (s3/minio), endpoint, bucket, accessKey и secretKey. POST с operation=verify проверяет существующий уровень и требует только name. PUT меняет ключи: нужны revision и confirmation с именем уровня. DELETE требует name, revision и confirmation."),
					Request:    tiersdto.TiersRequest{},
					Required:   []string{"name", "revision", "confirmation", "accessKey", "secretKey"},
					Controller: "HandleUpdateTier",
					Response:   resultdto.Success{}, Handler: func(app *runtime.
							State,
						w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleUpdateTier(app, w, r, s)
					},
				},
				{Suffix: "", Method: "DELETE", Mutation: routes.AdminLock, Summary: "Уровни хранения",
					Input:      routes.Inputs().Describe("POST без operation создаёт уровень: нужны name, type (s3/minio), endpoint, bucket, accessKey и secretKey. POST с operation=verify проверяет существующий уровень и требует только name. PUT меняет ключи: нужны revision и confirmation с именем уровня. DELETE требует name, revision и confirmation."),
					Request:    tiersdto.TiersRequest{},
					Required:   []string{"name", "revision", "confirmation"},
					Controller: "HandleRemoveTier",
					Response:   resultdto.Success{}, Handler: func(app *runtime.
							State,
						w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleRemoveTier(app, w, r, s)
					},
				},
			},
		},
	}
}
