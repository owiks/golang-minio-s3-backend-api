package operator

import (
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/operator/controllers"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/operations"
)

func GetRoutes() []routes.APIRoute {
	return []routes.APIRoute{{Path: "/api/operator/operations", Public: true, Tag: "operator", Operations: []routes.Operation{{
		Method: "GET", Operator: true, Controller: "OperatorOperationsStatus", Summary: "Сводка фоновой обработки API",
		Input:    routes.Inputs().Describe("Только отдельный API_OPERATOR_TOKEN, без cookie и пользовательских токенов. Без настройки endpoint отключён (404). queue.scope=store — общие счётчики сохранённых заданий; worker.scope=process — исполнители и наблюдения только ответившего процесса с текущего запуска. Времена успеха относятся к записанному успешному шагу; отсутствие времени не означает ошибку. Просроченные аренды считаются отдельно и не восстанавливаются этим запросом. Имена объектов, подключения, доступы и манифесты не возвращаются."),
		Response: operations.OperatorStatus{}, Handler: controllers.Status,
	}}}}
}
