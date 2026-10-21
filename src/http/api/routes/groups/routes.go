package groups

import (
	"net/http"
	"strings"

	"minio-web/api/src/http/api/common"
	"minio-web/api/src/http/api/routes"
	"minio-web/api/src/http/api/routes/groups/controllers"
	httpvalidation "minio-web/api/src/http/validation"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/identities"
	"minio-web/api/src/services/runtime"
)

func GetRoutes() []routes.APIRoute {
	return []routes.APIRoute{
		{
			Path: "admin/group-summaries",
			Tag:  "groups",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Сведения о группах",
					Input:      routes.Inputs(),
					Controller: "HandleGroupSummaries",
					Response:   []identities.GroupSummary{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						if r.Method != "GET" {
							return apperror.New(405, "MethodNotAllowed", "Операция управления не поддерживается.")
						}
						return controllers.HandleGroupSummaries(app, w, r, s)
					}},
			},
		},
		{
			Path: "admin/groups",
			Tag:  "groups",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Группы",
					Input:      routes.Inputs(),
					Controller: "HandleListGroups",
					Response:   []string{}, Handler: func(app *runtime.
							State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleListGroups(app, w, r, s)
					},
				},
				{Suffix: "", Method: "POST", Mutation: routes.IAMLock, InvalidatesAccess: true, Summary: "Группы",
					Input:      routes.Inputs().Describe("MinIO не поддерживает атомарное создание группы только при отсутствии имени. acknowledgeNonAtomicCreate=true подтверждает риск добавления участников в группу, созданную внешним клиентом после проверки. Уже найденная группа не изменяется."),
					Controller: "HandleCreateGroup",
					Request:    identities.CreateGroupRequest{},
					Response:   common.Success{},
					Required:   []string{"name", "members", "acknowledgeNonAtomicCreate"}, Handler: func(app *runtime.
							State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleCreateGroup(app, w, r, s)
					},
				},
				{Suffix: "{name}", Method: "GET", Summary: "Группы",
					Input:      routes.Inputs(),
					Controller: "HandleGetGroup",
					Response:   identities.GroupDetails{}, Handler: func(app *runtime.
							State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleGetGroup(app, w, r, s, strings.SplitN(rest, "/", 2)[0])
					},
				},
				{Suffix: "{name}", Method: "DELETE",
					Request: httpvalidation.ConfirmationRequest{}, Mutation: routes.IAMLock, InvalidatesAccess: true, Summary: "Группы",
					Input:      routes.Inputs(),
					Controller: "HandleDeleteGroup",
					Response:   common.Success{}, Handler: func(app *runtime.
							State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleDeleteGroup(app, w, r, s, strings.SplitN(rest, "/", 2)[0])
					},
				},
				{Suffix: "{name}/status", Method: "PUT", Mutation: routes.IAMLock, InvalidatesAccess: true, Summary: "Группы",
					Input:      routes.Inputs(),
					Controller: "HandleSetGroupStatus",
					Request:    identities.StatusRequest{},
					Response:   common.Success{}, Handler: func(app *runtime.
							State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleSetGroupStatus(app, w, r, s, strings.SplitN(rest, "/", 2)[0])
					},
				},
				{Suffix: "{name}/members", Method: "POST", Mutation: routes.IAMLock, InvalidatesAccess: true, Summary: "Группы",
					Input:      routes.Inputs(),
					Controller: "HandleUpdateGroupMembers",
					Request:    identities.GroupMembersRequest{},
					Response:   common.Success{}, Handler: func(app *runtime.
							State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleUpdateGroupMembers(app, w, r, s, strings.SplitN(rest, "/", 2)[0])
					},
				},
			},
		},
	}
}
