package transfers

import (
	"net/http"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/transfers/controllers"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/objects/write"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	transfersdto "github.com/owiks/golang-minio-s3-backend-api/src/services/transfers"
	transferssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/transfers"
)

func GetRoutes() []routes.APIRoute {
	return []routes.APIRoute{
		{
			Path: "objects/transfer-list",
			Tag:  "transfers",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Выбор назначения",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("prefix", true, 1024)),
					Controller: "HandleTransferList",
					Response:   transfersdto.TransferItems{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleTransferList(app, w, r, s)
					}},
			},
		},
		{
			Path: "objects/transfer-prepare",
			Tag:  "transfers",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Проверка копирования и перемещения",
					Input:      routes.Inputs(routes.Query("bucket", true, 63), routes.Query("key", true, 1024)),
					Controller: "HandleTransferPrepare",
					Response:   objectwrite.FileState{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleTransferPrepare(app, w, r, s)
					}},
			},
		},
		{
			Path: "objects/transfer",
			Tag:  "transfers",
			Operations: []routes.Operation{
				{Suffix: "", Method: "POST", Long: true, Summary: "Копирование и перемещение",
					Input:      routes.Inputs(routes.Header("Idempotency-Key", false).Describe("Optional request key: 1–128 visible ASCII characters; retained for 24 hours. An uncertain command is never automatically repeated.")),
					Controller: "HandleFileTransfer",
					Request:    transferssvc.FileTransferRequest{},
					Response:   transferssvc.TransferResult{},
					Required:   []string{"sourceBucket", "sourceKey", "sourceRevision", "destinationBucket", "destinationKey", "destinationRevision"}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleFileTransfer(app, w, r, s)
					}},
			},
		},
	}
}
