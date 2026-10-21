package objects

import (
	"net/http"

	"minio-web/api/src/http/api/routes"
	"minio-web/api/src/http/api/routes/objects/controllers"
	bucketsdto "minio-web/api/src/services/buckets"
	filesdto "minio-web/api/src/services/files"
	objectsdto "minio-web/api/src/services/objects"
	resultdto "minio-web/api/src/services/result"
	"minio-web/api/src/services/runtime"
)

func GetRoutes() []routes.APIRoute {
	return []routes.APIRoute{
		{
			Path: "objects/content",
			Tag:  "objects",
			Operations: []routes.Operation{
				{Method: "GET", Summary: "Текст файла",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("key", true, 1024)),
					Controller: "HandleGetObjectContent",
					Response:   objectsdto.Content{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleGetObjectContent(app, w, r, s)
					}},
				{Method: "PUT", Summary: "Редактирование текста файла",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("key", true, 1024)),
					Request:    objectsdto.ObjectContentRequest{},
					Required:   []string{"revision"},
					Controller: "HandleUpdateObjectContent",
					Response:   resultdto.Success{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleUpdateObjectContent(app, w, r, s)
					}},
			},
		},
		{
			Path: "buckets",
			Tag:  "objects",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Доступные бакеты",
					Input:      routes.Inputs(),
					Controller: "HandleListBuckets",
					Response:   []objectsdto.BucketRow{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleListBuckets(app, w, r, s)
					}},
			},
		},
		{
			Path: "objects",
			Tag:  "objects",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Список файлов и папок",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("prefix", false, 1024), routes.Query("token", false, 8192)),
					Controller: "HandleListObjects",
					Response:   objectsdto.ObjectPage{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleListObjects(app, w, r, s)
					}},
			},
		},
		{
			Path: "objects/info",
			Tag:  "objects",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Сведения о файле",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("key", true, 1024)),
					Controller: "HandleGetObjectInfo",
					Response:   filesdto.ObjectInfo{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleGetObjectInfo(app, w, r, s)
					}},
			},
		},
		{
			Path: "objects/preview",
			Tag:  "objects",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Предпросмотр файла",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("key", true, 1024)),
					Controller: "HandleGetObjectPreview",
					Responses:  []routes.Response{{Status: 200, MediaType: "text/plain", Binary: true}, {Status: 200, MediaType: "image/*", Binary: true}, {Status: 200, MediaType: "application/pdf", Binary: true}}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleGetObjectPreview(app, w, r, s)
					}},
			},
		},
		{
			Path: "objects/url",
			Tag:  "objects",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Подписанная ссылка",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("key", true, 1024), routes.EnumQuery("mode", true, "download", "share", "media"), routes.IntegerQuery("expiresIn", false, 1, 604800).Describe("Обязателен только при mode=share; в остальных режимах срок задаёт сервер.")),
					Controller: "HandleGetObjectLink",
					Response:   filesdto.ObjectLink{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleGetObjectLink(app, w, r, s)
					}},
			},
		},
		{
			Path: "objects/properties",
			Tag:  "objects",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Свойства и метки файла",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("key", true, 1024)),
					Controller: "HandleGetObjectProperties",
					Response:   objectsdto.ObjectProperties{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleGetObjectProperties(app, w, r, s)
					}},
				{Suffix: "", Method: "PUT", Long: true, Summary: "Свойства и метки файла",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("key", true, 1024)),
					Request:    objectsdto.ObjectPropertiesRequest{},
					Required:   []string{"revision"},
					Controller: "HandleUpdateObjectProperties",
					Response:   objectsdto.PropertiesUpdated{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleUpdateObjectProperties(app, w, r, s)
					}},
			},
		},
		{
			Path: "objects/checksum",
			Tag:  "objects",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Проверка целостности",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("key", true, 1024)),
					Controller: "HandleGetChecksum",
					Response:   objectsdto.ChecksumInfo{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleGetChecksum(app, w, r, s)
					}},
				{Suffix: "", Method: "POST",
					Request: controllers.ObjectChecksumRequest{}, Long: true, Summary: "Проверка целостности",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("key", true, 1024)),
					Controller: "HandleCalculateChecksum",
					Responses:  []routes.Response{{Status: 200, MediaType: "application/x-ndjson", Model: resultdto.StreamEvent[objectsdto.ChecksumResult]{}}}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleCalculateChecksum(app, w, r, s)
					}},
			},
		},
		{
			Path: "objects/usage",
			Tag:  "objects",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Анализ места",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("prefix", false, 1024), routes.Query("token", false, 16384), routes.EnumQuery("versions", false, "0", "1").Describe("1 — включить версии и маркеры удаления; по умолчанию только текущие объекты.")),
					Controller: "HandleObjectUsage",
					Response:   filesdto.ObjectUsage{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleObjectUsage(app, w, r, s)
					}},
			},
		},
		{
			Path: "admin/object-delete",
			Tag:  "objects",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Удаление выбранных файлов",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("key", true, 1024)),
					Controller: "HandleInspectObjectDeletion",
					Response:   objectsdto.DeletionInfo{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleInspectObjectDeletion(app, w, r, s)
					}},
				{Suffix: "", Method: "DELETE", Summary: "Удаление выбранных файлов",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("key", true, 1024)),
					Request:    objectsdto.ObjectDeleteRequest{},
					Required:   []string{"revision", "confirmation"},
					Controller: "HandleDeleteObject",
					Response:   resultdto.Success{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleDeleteObject(app, w, r, s)
					}},
			},
		},
		{
			Path: "admin/object-versions",
			Tag:  "objects",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Версии и защита файла",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("key", true, 1024), routes.Query("cursor", false, 10923).Describe("Непрозрачный курсор из предыдущей страницы версий; Base64URL без padding.")),
					Controller: "HandleListObjectVersions",
					Response:   objectsdto.VersionsPage{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleListObjectVersions(app, w, r, s)
					}},
				{Suffix: "url", Method: "GET", Summary: "Версии и защита файла",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("key", true, 1024), routes.Query("version", true, 1024)),
					Controller: "HandleGetVersionLink",
					Response:   resultdto.Link{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleGetVersionLink(app, w, r, s)
					}},
				{Suffix: "protection", Method: "GET", Summary: "Версии и защита файла",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("key", true, 1024), routes.Query("version", true, 1024)),
					Controller: "HandleGetVersionProtection",
					Response:   filesdto.VersionProtection{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleGetVersionProtection(app, w, r, s)
					}},
				{Suffix: "retention", Method: "PUT", Summary: "Версии и защита файла",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("key", true, 1024), routes.Query("version", true, 1024)),
					Request:    filesdto.ObjectVersionsRequest{},
					Required:   []string{"revision", "confirmation"},
					Controller: "HandleUpdateVersionProtection",
					Response:   bucketsdto.DetailField{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleUpdateVersionProtection(app, w, r, s, filesdto.RetentionProtection)
					}},
				{Suffix: "hold", Method: "PUT", Summary: "Версии и защита файла",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("key", true, 1024), routes.Query("version", true, 1024)),
					Request:    filesdto.ObjectVersionsRequest{},
					Required:   []string{"revision", "confirmation", "status"},
					Controller: "HandleUpdateVersionProtection",
					Response:   bucketsdto.DetailField{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleUpdateVersionProtection(app, w, r, s, filesdto.LegalHoldProtection)
					}},
			},
		},
	}
}
