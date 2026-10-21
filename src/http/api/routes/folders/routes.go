package folders

import (
	"net/http"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/folders/controllers"
	foldersdto "github.com/owiks/golang-minio-s3-backend-api/src/services/folders"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/folders/share"
	objectsdto "github.com/owiks/golang-minio-s3-backend-api/src/services/objects"
	resultdto "github.com/owiks/golang-minio-s3-backend-api/src/services/result"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

func GetRoutes() []routes.APIRoute {
	return []routes.APIRoute{
		{Path: "/api/shared-folder", Public: true, Tag: "folders", Operations: []routes.Operation{
			{Method: "GET", Long: true, Summary: "Просмотр папки по ссылке",
				Input:      routes.Inputs(routes.Header("Authorization", true).Describe("Bearer <токен общей папки>."), routes.EnumQuery("mode", false, "info", "list", "preview", "ticket"), routes.Query("prefix", false, 1024), routes.Query("cursor", false, 8192), routes.Query("key", false, 1024).Describe("Обязателен при mode=preview или ticket."), routes.EnumQuery("kind", false, "download", "media").Describe("Обязателен при mode=ticket.")).Describe("mode по умолчанию info. Для list укажите prefix внутри общей папки; для preview и ticket нужен key, для ticket также kind."),
				Controller: "HandlePublicFolder",
				Responses:  []routes.Response{{Status: 200, MediaType: "application/json", Variants: []any{foldersdto.PublicFolderInfo{}, objectsdto.ObjectPage{}, resultdto.Link{}}}, {Status: 200, MediaType: "text/plain", Binary: true}, {Status: 200, MediaType: "image/*", Binary: true}, {Status: 200, MediaType: "application/pdf", Binary: true}}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
					return controllers.HandlePublicFolder(app, w, r)
				}},
		},
		},
		{Path: "/api/shared-folder/file", Public: true, Tag: "folders", Operations: []routes.Operation{
			{Suffix: "{ticket}", Method: "HEAD", Long: true, Summary: "Сведения о файле по ссылке",
				Input:      routes.Inputs(routes.Header("Range", false).Describe("Только для media-билета; один диапазон bytes=start-end, bytes=start- или bytes=-suffix.")),
				Controller: "HandleHeadPublicFolderFile",
				Responses:  []routes.Response{{Status: 200}}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
					return controllers.HandleHeadPublicFolderFile(app, w, r, rest)
				}},
			{Suffix: "{ticket}", Method: "GET", Long: true, Summary: "Скачивание файла по ссылке",
				Input:      routes.Inputs(routes.Header("Range", false).Describe("Только для media-билета; один диапазон bytes=start-end, bytes=start- или bytes=-suffix.")),
				Controller: "HandleDownloadPublicFolderFile",
				Responses:  []routes.Response{{Status: 200, MediaType: "application/octet-stream", Binary: true}, {Status: 200, MediaType: "audio/*", Binary: true}, {Status: 200, MediaType: "video/*", Binary: true}, {Status: 206, MediaType: "application/octet-stream", Binary: true}, {Status: 206, MediaType: "audio/*", Binary: true}, {Status: 206, MediaType: "video/*", Binary: true}}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
					return controllers.HandleDownloadPublicFolderFile(app, w, r, rest)
				}},
		},
		},
		{
			Path: "objects/folders",
			Tag:  "folders",
			Operations: []routes.Operation{
				{Suffix: "", Method: "POST", Mutation: routes.AfterDecode, Summary: "Создание папки",
					Input:      routes.Inputs(),
					Request:    foldersdto.CreateFolderRequest{},
					Required:   []string{"bucket", "name"},
					Controller: "HandleCreateFolder", Status: 201,
					Response: foldersdto.CreatedFolder{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleCreateFolder(app, w, r, s)
					}},
			},
		},
		{
			Path: "objects/folder-delete",
			Tag:  "folders",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Удаление папки с содержимым",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("prefix", true, 1024), routes.Query("key", false, 1024).Describe("Без key — план папки; с key — состояние выбранного объекта плана.")),
					Controller: "HandleInspectFolderDeletion",
					Responses:  []routes.Response{{Status: 200, MediaType: "application/json", Variants: []any{foldersdto.FolderDeletionInfo{}, foldersdto.FolderObjectInfo{}}}}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleInspectFolderDeletion(app, w, r, s)
					}},
				{Suffix: "", Method: "DELETE", Summary: "Удаление папки с содержимым",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("prefix", true, 1024), routes.Query("key", true, 1024)),
					Request:    foldersdto.FolderDeleteRequest{},
					Required:   []string{"revision", "confirmation"},
					Controller: "HandleDeleteFolderObject",
					Response:   resultdto.Success{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleDeleteFolderObject(app, w, r, s)
					}},
			},
		},
		{
			Path: "objects/folder-shares",
			Tag:  "folders",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Ссылки на папки",
					Input:      routes.Inputs(routes.Query("bucket", false, 0), routes.Query("prefix", false, 0)).Describe("POST: expiresIn от 60 до 86400 секунд; prefix пустой для корня или заканчивается /. DELETE отзывает ссылку по token."),
					Controller: "HandleListFolderShares",
					Response:   []foldershare.FolderShareSummary{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleListFolderShares(app, w, r, s)
					}},
				{Suffix: "", Method: "POST", Summary: "Ссылки на папки",
					Input:      routes.Inputs().Describe("POST: expiresIn от 60 до 86400 секунд; prefix пустой для корня или заканчивается /. DELETE отзывает ссылку по token."),
					Request:    foldersdto.FolderSharesRequest{},
					Required:   []string{"bucket", "expiresIn"},
					Controller: "HandleCreateFolderShare",
					Response:   foldershare.View{}, Status: 201, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleCreateFolderShare(app, w, r, s)
					}},
				{Suffix: "", Method: "DELETE", Summary: "Ссылки на папки",
					Input:      routes.Inputs(routes.Query("token", true, 0)).Describe("POST: expiresIn от 60 до 86400 секунд; prefix пустой для корня или заканчивается /. DELETE отзывает ссылку по token."),
					Controller: "HandleRevokeFolderShare",
					Response:   resultdto.Success{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleRevokeFolderShare(app, w, r, s)
					}},
			},
		},
	}
}
