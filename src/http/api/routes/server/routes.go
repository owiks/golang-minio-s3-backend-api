package server

import (
	"net/http"

	"minio-web/api/src/http/api/routes"
	"minio-web/api/src/http/api/routes/server/controllers"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
	resultdto "minio-web/api/src/services/result"
	"minio-web/api/src/services/runtime"
	serverdto "minio-web/api/src/services/server"
)

func GetRoutes() []routes.APIRoute {
	return []routes.APIRoute{
		{
			Path: "admin/server-config",
			Tag:  "server",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Настройки MinIO",
					Input:      routes.Inputs(routes.EnumQuery("subsystem", true, "api", "scanner", "heal")),
					Controller: "HandleReadServerConfig",
					Response:   serverdto.ServerConfigView{}, Handler: func(app *runtime.
							State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleReadServerConfig(app, w, r, s)
					},
				},
				{Suffix: "", Method: "PUT", Mutation: routes.AdminLock, Summary: "Настройки MinIO",
					Input:      routes.Inputs(routes.EnumQuery("subsystem", true, "api", "scanner", "heal")).Describe("Передайте revision и values (1–30 доступных полей) либо restoreId без values. Поля ENV изменяются в конфигурации запуска."),
					Request:    serverdto.ConfigChangeRequest{},
					Required:   []string{"revision"},
					Controller: "HandleSaveServerConfig",
					Response:   serverdto.ConfigChangeResult{}, Handler: func(app *runtime.
							State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleSaveServerConfig(app, w, r, s)
					},
				},
			},
		},
		{
			Path: "admin/server-info",
			Tag:  "server",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Состояние сервера",
					Input:      routes.Inputs(),
					Controller: "HandleServerInfo",
					Response:   serverdto.ServerSnapshot{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						if r.Method != "GET" {
							return apperror.New(405, "MethodNotAllowed", messages.ManagementUnsupported)
						}
						return controllers.HandleServerInfo(app, w, r, s)
					}},
			},
		},
		{
			Path: "admin/server-metrics",
			Tag:  "server",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Нагрузка S3",
					Input:      routes.Inputs(),
					Controller: "HandleServerMetrics",
					Response:   serverdto.LiveMetrics{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleServerMetrics(app, w, r, s)
					}},
			},
		},
		{
			Path: "admin/server-pools",
			Tag:  "server",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Пулы хранения",
					Input:      routes.Inputs(),
					Controller: "HandleReadServerPools",
					Response:   serverdto.PoolsView{}, Handler: func(app *runtime.
							State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleReadServerPools(app, w, r, s)
					},
				},
				{Suffix: "", Method: "POST", Mutation: routes.AdminLock, Summary: "Пулы хранения",
					Input:      routes.Inputs(),
					Request:    serverdto.PoolChangeRequest{},
					Required:   []string{"revision", "operation", "confirmation"},
					Controller: "HandleChangeServerPools",
					Response:   resultdto.Success{}, Handler: func(app *runtime.
							State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleChangeServerPools(app, w, r, s)
					},
				},
			},
		},
		{
			Path: "admin/diagnostics",
			Tag:  "server",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Диагностика",
					Input:      routes.Inputs(routes.EnumQuery("kind", true, "logs", "trace"), routes.EnumQuery("level", true, "ALL", "ERROR"), routes.IntegerQuery("limit", true, 1, 200), routes.IntegerQuery("seconds", true, 1, 10)),
					Controller: "HandleDiagnostics",
					Response:   serverdto.DiagnosticsReport{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleDiagnostics(app, w, r, s)
					}},
			},
		},
	}
}
