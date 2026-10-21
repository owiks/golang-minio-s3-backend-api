package multipart

import (
	"net/http"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/multipart/controllers"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/uploads/multipart"
)

func GetRoutes() []routes.APIRoute {
	return []routes.APIRoute{{Path: "objects/multipart", Tag: "uploads", Operations: []routes.Operation{
		{Method: "POST", Long: true, Summary: "Создать управляемую multipart-загрузку", Controller: "HandleCreateMultipart", Request: multipart.CreateRequest{}, Required: []string{"bucket", "key", "revision", "size"}, Input: routes.Inputs().Describe("До 5 ГиБ; части 16 МиБ, последняя по остатку (пустой файл — одна часть 0 байт). Необязательная checksum: SHA256 COMPOSITE OBJECT. До 32 метаданных / 8 КиБ. Состояние в памяти, до конца сессии и не более часа; после перезапуска не восстанавливается. 202 create_uncertain: upload мог быть создан без полученного uploadId, запрос автоматически не повторяется."), Responses: []routes.Response{{Status: 201, MediaType: "application/json", Model: multipart.Snapshot{}}, {Status: 202, MediaType: "application/json", Model: multipart.Snapshot{}}}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
			return controllers.HandleCreate(app, w, r, s)
		}},
		{Suffix: "{operation}", Method: "GET", Summary: "Состояние и подтверждённые S3 части загрузки", Controller: "HandleInspectMultipart", Input: routes.Inputs().Describe("ID принадлежит текущей сессии. Источник частей — ListParts. Получите состояние после обрыва передачи перед повтором. complete_uncertain проверяется по уникальному маркеру, размеру и сохранённой checksum; отсутствия объекта недостаточно для повтора complete."), Responses: []routes.Response{{Status: 200, MediaType: "application/json", Model: multipart.Snapshot{}}, {Status: 202, MediaType: "application/json", Model: multipart.Snapshot{}}}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
			return controllers.HandleInspect(app, w, r, s, rest)
		}},
		{Suffix: "{operation}/parts/{part}", Method: "PUT", Long: true, BinaryRequest: true, Summary: "Передать часть управляемой загрузки через API", Controller: "HandleMultipartPart", Input: routes.Inputs(routes.Header("X-Upload-Size", true).AsInteger(0, 16777216), routes.Header("X-Part-Checksum-SHA256", true).Describe("Canonical padded base64 SHA256 тела части; обязательна нативная проверка S3. Content-Length равен X-Upload-Size. Повтор с другим SHA256 запрещён; после потери ответа проверяется ListParts. Ошибка части не отменяет загрузку.")), Response: multipart.Snapshot{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
			return controllers.HandlePart(app, w, r, s, rest)
		}},
		{Suffix: "{operation}/parts/{part}/sign", Method: "POST", Summary: "Подписать прямую передачу конкретной части в S3", Controller: "HandleSignMultipartPart", Request: multipart.PartRequest{}, Required: []string{"checksum"}, Input: routes.Inputs().Describe("SHA256 PART FULL_OBJECT base64; PUT к public endpoint, фиксированным key/uploadId/номеру части. TTL до 2 минут и не дольше сессии. URL — секрет, не логировать. JavaScript передаёт только возвращённые headers; Content-Length не подписывается и размер проверяется перед complete. IAM окончательно проверяет S3. Уже выданный URL не отзывается мгновенно; CORS бакета автоматически не меняется."), Response: multipart.SignedPart{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
			return controllers.HandleSign(app, w, r, s, rest)
		}},
		{Suffix: "{operation}/complete", Method: "POST", Long: true, Summary: "Проверить части и условно завершить загрузку", Controller: "HandleCompleteMultipart", Input: routes.Inputs().Describe("Проверяет S3 части, порядок, размеры, SHA256, исходную ревизию назначения. Native If-Match/If-None-Match применяется к CompleteMultipartUpload. DestinationChanged не повторяется без условия. После подтверждённого успеха повтор возвращает сохранённый receipt без upstream complete. 202 complete_uncertain требует чтения состояния; новая загрузка автоматически не создаётся."), Responses: []routes.Response{{Status: 200, MediaType: "application/json", Model: multipart.Snapshot{}}, {Status: 202, MediaType: "application/json", Model: multipart.Snapshot{}}}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
			return controllers.HandleComplete(app, w, r, s, rest)
		}},
		{Suffix: "{operation}", Method: "DELETE", Long: true, Summary: "Отменить принадлежащую сессии multipart-загрузку", Controller: "HandleAbortMultipart", Input: routes.Inputs().Describe("Отмена сериализована с part/complete. Повтор подтверждённой отмены не вызывает S3. 202 abort_uncertain означает неподтверждённый результат; очистка делает до 3 ограниченных попыток. При complete_uncertain сначала проверяется результат. После аварии API могут остаться незавершённые multipart; используйте административный список/Lifecycle."), Responses: []routes.Response{{Status: 200, MediaType: "application/json", Model: multipart.Snapshot{}}, {Status: 202, MediaType: "application/json", Model: multipart.Snapshot{}}}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
			return controllers.HandleAbort(app, w, r, s, rest)
		}},
	}}}
}
