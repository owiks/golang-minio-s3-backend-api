package uploads

import (
	"net/http"

	"minio-web/api/src/http/api/routes"
	"minio-web/api/src/http/api/routes/uploads/controllers"
	"minio-web/api/src/services/objects/write"
	resultdto "minio-web/api/src/services/result"
	"minio-web/api/src/services/runtime"
	uploadsdto "minio-web/api/src/services/uploads"
	"minio-web/api/src/services/uploads/invite"
)

func GetRoutes() []routes.APIRoute {
	return []routes.APIRoute{
		{Path: "/api/receive", Public: true, Tag: "uploads", Operations: []routes.Operation{
			{Method: "GET", Long: true, Summary: "Сведения о приглашении загрузки",
				Input:      routes.Inputs(routes.Header("Authorization", true).Describe("Bearer <токен приглашения загрузки>.")),
				Controller: "HandleInspectUploadInvite",
				Response:   uploadsdto.InviteInfo{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
					return controllers.HandleInspectUploadInvite(app, w, r)
				}},
			{Method: "PUT", BinaryRequest: true, Long: true, Summary: "Загрузка файла по приглашению",
				Input:      routes.Inputs(routes.Header("Authorization", true).Describe("Bearer <токен приглашения загрузки>."), routes.Query("name", true, 0).Describe("Имя без / и обратной косой черты; полный ключ с префиксом — до 1024 байт."), routes.Header("Origin", true), routes.Header("X-Upload-Size", true).AsInteger(0, 1073741824).Describe("Размер в байтах, равен Content-Length и не больше лимита приглашения.")),
				Controller: "HandleReceiveFile",
				Response:   resultdto.Success{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
					return controllers.HandleReceiveFile(app, w, r)
				}},
		},
		},
		{
			Path: "objects/import",
			Tag:  "uploads",
			Operations: []routes.Operation{
				{Suffix: "", Method: "POST", Long: true, Summary: "Импорт по URL",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("key", true, 1024)),
					Request:    uploadsdto.ImportRequest{},
					Required:   []string{"revision", "url"},
					Controller: "HandleRemoteImport",
					Responses:  []routes.Response{{Status: 200, MediaType: "application/x-ndjson", Model: resultdto.StreamEvent[uploadsdto.ImportResult]{}}}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleRemoteImport(app, w, r, s)
					}},
			},
		},
		{
			Path: "objects/invites",
			Tag:  "uploads",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Приглашения для загрузки",
					Input:      routes.Inputs(routes.Query("bucket", false, 0), routes.Query("prefix", false, 0)),
					Controller: "HandleListUploadInvites",
					Response:   []uploadinvite.UploadInviteSummary{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleListUploadInvites(app, w, r, s)
					}},
				{Suffix: "", Method: "POST", Summary: "Приглашения для загрузки",
					Input:      routes.Inputs(),
					Request:    uploadsdto.UploadInvitesRequest{},
					Required:   []string{"bucket", "expiresIn", "maxSize", "maxFiles"},
					Controller: "HandleCreateUploadInvite",
					Response:   uploadinvite.View{}, Status: 201, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleCreateUploadInvite(app, w, r, s)
					}},
				{Suffix: "", Method: "DELETE", Summary: "Приглашения для загрузки",
					Input:      routes.Inputs(routes.Query("token", true, 0)),
					Controller: "HandleRevokeUploadInvite",
					Response:   resultdto.Success{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleRevokeUploadInvite(app, w, r, s)
					}},
			},
		},
		{
			Path: "objects/prepare",
			Tag:  "uploads",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Загрузка файлов",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("key", true, 1024)),
					Controller: "HandlePrepareFile",
					Response:   objectwrite.FileState{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandlePrepareFile(app, w, r, s)
					}},
			},
		},
		{
			Path: "objects/upload",
			Tag:  "uploads",
			Operations: []routes.Operation{
				{Suffix: "", Method: "PUT", BinaryRequest: true, Long: true, Summary: "Загрузка файлов",
					Input: routes.Inputs(
						routes.Query("bucket", true, 63), routes.Query("key", true, 1024),
						routes.Header("X-Object-Revision", true).Describe("Ревизия из objects/prepare; для отсутствующего объекта — absent."),
						routes.Header("X-Upload-Size", true).AsInteger(0, 5368709120).Describe("Размер тела в байтах, должен совпадать с Content-Length; до 5 ГиБ."),
						routes.Header("X-Upload-Checksum-Algorithm", false).Describe("Необязательная ожидаемая checksum: MD5, SHA1, SHA256, CRC32 или CRC32C. При указании обязателен X-Upload-Checksum-Value. Проверка до записи использует общий бюджет временного диска."),
						routes.Header("X-Upload-Checksum-Value", false).Describe("Каноническое base64-значение хэша всего объекта. Несовпадение отклоняется до записи в S3."),
						routes.Header("X-Upload-Checksum-Encoding", false).Describe("Только base64; значение по умолчанию — base64."),
						routes.Header("X-Upload-Checksum-Type", false).Describe("Только FULL_OBJECT. Режим COMPOSITE не поддерживается обычной загрузкой. Receipt различает api_verified и подтверждение upload_response; ETag не является checksum."),
					),
					Controller: "HandleWriteFile",
					Response:   uploadsdto.UploadReceipt{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleWriteFile(app, w, r, s)
					}},
			},
		},
		{
			Path: "admin/incomplete-uploads",
			Tag:  "uploads",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Незавершённые загрузки",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("prefix", false, 1024), routes.Query("keyMarker", false, 2048), routes.Query("uploadIdMarker", false, 8192), routes.EnumQuery("summary", false, "0", "1")),
					Controller: "HandleListIncompleteUploads",
					Response:   uploadsdto.IncompleteUploadsPage{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleListIncompleteUploads(app, w, r, s)
					}},
				{Suffix: "parts", Method: "GET", Summary: "Незавершённые загрузки",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("key", true, 1024), routes.Query("uploadId", true, 4096), routes.EnumQuery("summary", false, "0", "1")),
					Controller: "HandleInspectIncompleteUpload",
					Response:   uploadsdto.IncompleteUploadState{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleInspectIncompleteUpload(app, w, r, s)
					}},
				{Suffix: "abort", Method: "POST", Mutation: routes.AdminLock, Summary: "Незавершённые загрузки",
					Input:      routes.Inputs(),
					Request:    uploadsdto.IncompleteUploadsRequest{},
					Required:   []string{"bucket", "key", "uploadId", "revision", "confirmation"},
					Controller: "HandleAbortIncompleteUpload",
					Response:   uploadsdto.AbortUploadResult{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleAbortIncompleteUpload(app, w, r, s)
					}},
			},
		},
	}
}
