package operations

import (
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/operations/controllers"
)

func GetRoutes() []routes.APIRoute {
	key := routes.Inputs(routes.Header("Idempotency-Key", false).Describe("Optional request key retained for 24 hours. Repeated submissions return the existing operation; a different payload conflicts."))
	return []routes.APIRoute{{Path: "operations", Tag: "operations", Operations: []routes.Operation{
		{Method: "GET", Summary: "Список фоновых операций текущего доступа", Controller: "ListOperations", Input: routes.Inputs(), Response: []controllers.View{}, Handler: controllers.List},
		{Suffix: "transfers", Method: "POST", Mutation: routes.NoLock, Status: 202, Scopes: []string{"objects:read", "objects:write"}, Summary: "Создать фоновое копирование или перемещение", Controller: "CreateTransferOperation", Input: key, Request: controllers.TransfersRequest{}, Required: []string{"items"}, Response: controllers.View{}, Handler: controllers.CreateTransfers},
		{Suffix: "scans", Method: "POST", Mutation: routes.NoLock, Status: 202, Scopes: []string{"objects:read"}, Summary: "Создать сохраняемый анализ размера бакета", Controller: "CreateScanOperation", Input: key, Request: controllers.ScanRequest{}, Required: []string{"bucket"}, Response: controllers.View{}, Handler: controllers.CreateScan},
		{Suffix: "{operationID}", Method: "GET", Summary: "Состояние и результаты фоновой операции", Controller: "GetOperation", Input: routes.Inputs(), Response: controllers.View{}, Handler: controllers.Get},
		{Suffix: "{operationID}/cancel", Method: "POST", Mutation: routes.NoLock, Summary: "Отменить собственную фоновую операцию", Controller: "CancelOperation", Input: routes.Inputs(), Request: controllers.RevisionRequest{}, Required: []string{"revision"}, Response: common.Success{}, Handler: controllers.Cancel},
		{Suffix: "{operationID}/retry", Method: "POST", Mutation: routes.NoLock, Summary: "Продолжить операцию после определённой ошибки", Controller: "RetryOperation", Input: routes.Inputs(), Request: controllers.RevisionRequest{}, Required: []string{"revision"}, Response: controllers.View{}, Handler: controllers.Retry},
	}}}
}
