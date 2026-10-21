package archives

import (
	"net/http"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/archives/controllers"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	archivesdto "github.com/owiks/golang-minio-s3-backend-api/src/services/archives"
	archiveplan "github.com/owiks/golang-minio-s3-backend-api/src/services/archives/plan"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	resultdto "github.com/owiks/golang-minio-s3-backend-api/src/services/result"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

func GetRoutes() []routes.APIRoute {
	return []routes.APIRoute{
		{
			Path: "objects/gzip",
			Tag:  "archives",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Просмотр GZIP",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("key", true, 1024)),
					Controller: "HandleGzipView",
					Response:   archivesdto.GzipContent{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleGzipView(app, w, r, s)
					}},
			},
		},
		{
			Path: "objects/zip",
			Tag:  "archives",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Long: true, Summary: "Просмотр и извлечение ZIP",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("key", true, 1024)),
					Controller: "HandleInspectZIP",
					Response:   archivesdto.ZIPContents{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleInspectZIP(app, w, r, s)
					}},
				{Suffix: "", Method: "POST", Long: true, Summary: "Просмотр и извлечение ZIP",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("key", true, 1024)),
					Request:    archivesdto.ZipExplorerRequest{},
					Required:   []string{"revision", "bucket", "key", "destinationRevision"},
					Controller: "HandleExtractZIPEntry",
					Response:   archivesdto.ExtractedEntry{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleExtractZIPEntry(app, w, r, s)
					}},
			},
		},
		{
			Path: "objects/archive",
			Tag:  "archives",
			Operations: []routes.Operation{
				{Suffix: "", Method: "POST", Long: true, Summary: "Подготовка скачивания ZIP",
					Input:      routes.Inputs().Describe("Передайте bucket и ровно один способ выбора: keys, entries или prefix (пустая строка — весь бакет). Список: до 1000 файлов, включая папки после раскрытия."),
					Request:    archivesdto.PrepareArchiveRequest{},
					Required:   []string{"bucket"},
					Controller: "HandlePrepareArchive",
					Response:   archiveplan.ArchiveSnapshot{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandlePrepareArchive(app, w, r, s)
					}},
			},
		},
		{
			Path: "objects/archive",
			Tag:  "archives",
			Operations: []routes.Operation{
				{Suffix: "{ticket}", Method: "GET", Long: true, Summary: "Скачивание и отмена ZIP",
					Input:      routes.Inputs(),
					Controller: "HandleDownloadArchive",
					Responses:  []routes.Response{{Status: 200, MediaType: "application/zip", Binary: true, Localized: true}}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						if rest == "" {
							return apperror.New(404, "NotFound", messages.RouteNotFound)
						}
						return controllers.HandleDownloadArchive(app, w, r, s, rest)
					}},
				{Suffix: "{ticket}", Method: "PUT", Summary: "Параметры ZIP",
					Input:      routes.Inputs(),
					Request:    archivesdto.ArchiveRequest{},
					Controller: "HandleConfigureArchive",
					Response:   archiveplan.ArchiveSnapshot{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						if rest == "" {
							return apperror.New(404, "NotFound", messages.RouteNotFound)
						}
						return controllers.HandleConfigureArchive(app, w, r, s, rest)
					}},
				{Suffix: "{ticket}", Method: "DELETE", Summary: "Скачивание и отмена ZIP",
					Input:      routes.Inputs(),
					Controller: "HandleCancelArchive",
					Response:   archiveplan.ArchiveSnapshot{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						if rest == "" {
							return apperror.New(404, "NotFound", messages.RouteNotFound)
						}
						return controllers.HandleCancelArchive(app, w, r, s, rest)
					}},
			},
		},
		{
			Path: "objects/archive-status",
			Tag:  "archives",
			Operations: []routes.Operation{
				{Suffix: "{ticket}", Method: "GET", Summary: "Состояние ZIP",
					Input:      routes.Inputs(),
					Controller: "HandleArchiveStatus",
					Response:   archiveplan.ArchiveSnapshot{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						if rest == "" {
							return apperror.New(404, "NotFound", messages.RouteNotFound)
						}
						return controllers.HandleArchiveStatus(app, w, r, s, rest)
					}}, {Suffix: "{ticket}", Method: "DELETE", Summary: "Удаление результата ZIP",
					Input:      routes.Inputs(),
					Controller: "HandleForgetArchive",
					Response:   resultdto.Success{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						if rest == "" {
							return apperror.New(404, "NotFound", messages.RouteNotFound)
						}
						return controllers.HandleForgetArchive(app, w, r, s, rest)
					}},
			},
		},
	}
}
