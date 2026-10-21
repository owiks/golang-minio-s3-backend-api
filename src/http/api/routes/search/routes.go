package search

import (
	"net/http"

	"minio-web/api/src/http/api/routes"
	"minio-web/api/src/http/api/routes/search/controllers"
	filesdto "minio-web/api/src/services/files"
	"minio-web/api/src/services/runtime"
	searchdto "minio-web/api/src/services/search"
)

func GetRoutes() []routes.APIRoute {
	return []routes.APIRoute{
		{
			Path: "objects/search",
			Tag:  "search",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Поиск файлов по бакету",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("prefix", false, 1024), routes.Query("token", false, 8192), routes.Query("q", false, 256), routes.Query("extensions", false, 128), routes.IntegerQuery("minSize", false, 0, 9223372036854775807), routes.IntegerQuery("maxSize", false, 0, 9223372036854775807).Describe("Не меньше minSize."), routes.Query("after", false, 0).AsDate(), routes.Query("before", false, 0).AsDate().Describe("Дата включительно; не раньше after.")),
					Controller: "HandleFileSearch",
					Response:   searchdto.FileSearchResult{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleFileSearch(app, w, r, s)
					}},
			},
		},
		{
			Path: "objects/content-search",
			Tag:  "search",
			Operations: []routes.Operation{
				{Suffix: "", Method: "POST", Long: true, Summary: "Поиск по содержимому",
					Input:      routes.Inputs().Describe("maxSize: 1–1048576 байт; pageSize: 1–20 файлов; query: 1–256 байт UTF-8. Курсор token берётся из предыдущей страницы."),
					Request:    searchdto.ContentSearchBody{},
					Required:   []string{"bucket", "query", "maxSize", "pageSize"},
					Controller: "HandleContentSearch",
					Response:   filesdto.ContentSearchResult{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleContentSearch(app, w, r, s)
					}},
			},
		},
	}
}
