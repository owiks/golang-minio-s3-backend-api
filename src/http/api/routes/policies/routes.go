package policies

import (
	"net/http"
	"strings"

	"minio-web/api/src/http/api/common"
	"minio-web/api/src/http/api/routes"
	"minio-web/api/src/http/api/routes/policies/controllers"
	httpvalidation "minio-web/api/src/http/validation"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/identities"
	"minio-web/api/src/services/messages"
	"minio-web/api/src/services/runtime"
)

func GetRoutes() []routes.APIRoute {
	return []routes.APIRoute{
		{
			Path: "admin/policy-summaries",
			Tag:  "policies",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Сведения о политиках",
					Input:      routes.Inputs(),
					Controller: "HandlePolicySummaries",
					Response:   []identities.PolicySummary{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						if r.Method != "GET" {
							return apperror.New(405, "MethodNotAllowed", messages.ManagementUnsupported)
						}
						return controllers.HandlePolicySummaries(app, w, r, s)
					}},
			},
		},
		{
			Path: "admin/policies",
			Tag:  "policies",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "IAM-политики",
					Input:      routes.Inputs(),
					Controller: "HandleListPolicies",
					Response:   []string{}, Handler: func(app *runtime.
							State, w http.
							ResponseWriter, r *http.
							Request, s *runtime.Session, rest string) error {
						return controllers.HandleListPolicies(app, w, r, s)
					},
				},
				{Suffix: "", Method: "POST", Mutation: routes.IAMLock, InvalidatesAccess: true, Summary: "IAM-политики",
					Input:      routes.Inputs().Describe("MinIO не поддерживает атомарное создание политики только при отсутствии имени. acknowledgeNonAtomicCreate=true подтверждает риск перезаписи политики, созданной внешним клиентом после проверки, с сохранением назначений. Уже найденная политика не изменяется."),
					Controller: "HandleCreatePolicy",
					Request:    identities.PolicyRequest{},
					Response:   common.Success{},
					Required:   []string{"name", "document", "acknowledgeNonAtomicCreate"}, Handler: func(app *runtime.
							State, w http.
							ResponseWriter, r *http.
							Request, s *runtime.Session, rest string) error {
						return controllers.HandleCreatePolicy(app, w, r, s)
					},
				},
				{Suffix: "{name}", Method: "GET", Summary: "IAM-политики",
					Input:      routes.Inputs(),
					Controller: "HandleGetPolicy",
					Response:   identities.PolicyDetails{}, Handler: func(app *runtime.
							State, w http.
							ResponseWriter, r *http.
							Request, s *runtime.Session, rest string) error {
						return controllers.HandleGetPolicy(app, w, r, s, strings.SplitN(rest, "/", 2)[0])
					},
				},
				{Suffix: "{name}", Method: "PUT", Mutation: routes.IAMLock, InvalidatesAccess: true, Summary: "IAM-политики",
					Input:      routes.Inputs(),
					Controller: "HandleUpdatePolicy",
					Request:    identities.PolicyRequest{},
					Response:   common.Success{},
					Required:   []string{"document", "revision"}, Handler: func(app *runtime.
							State, w http.
							ResponseWriter, r *http.
							Request, s *runtime.Session, rest string) error {
						return controllers.HandleUpdatePolicy(app, w, r, s, strings.SplitN(rest, "/", 2)[0])
					},
				},
				{Suffix: "{name}", Method: "DELETE",
					Request: httpvalidation.ConfirmationRequest{}, Mutation: routes.IAMLock, InvalidatesAccess: true, Summary: "IAM-политики",
					Input:      routes.Inputs(),
					Controller: "HandleDeletePolicy",
					Response:   common.Success{}, Handler: func(app *runtime.
							State, w http.
							ResponseWriter, r *http.
							Request, s *runtime.Session, rest string) error {
						return controllers.HandleDeletePolicy(app, w, r, s, strings.SplitN(rest, "/", 2)[0])
					},
				},
			},
		},
	}
}
