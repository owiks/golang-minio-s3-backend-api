package batch

import (
	httpvalidation "github.com/owiks/golang-minio-s3-backend-api/src/http/validation"
	"net/http"
	"strings"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/batch/controllers"

	batchdto "github.com/owiks/golang-minio-s3-backend-api/src/services/batch"
	resultdto "github.com/owiks/golang-minio-s3-backend-api/src/services/result"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

func GetRoutes() []routes.APIRoute {
	return []routes.APIRoute{
		{
			Path: "admin/batch-jobs",
			Tag:  "batch",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Пакетные задания",
					Input:      routes.Inputs(),
					Controller: "HandleListBatchJobs",
					Response:   []batchdto.Job{}, Handler: func(app *runtime.
							State,
						w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleListBatchJobs(app, w, r, s)
					},
				},
				{Suffix: "", Method: "POST", Mutation: routes.AdminLock, Summary: "Пакетные задания",
					Input:      routes.Inputs(),
					Request:    batchdto.BatchInput{},
					Required:   []string{"type", "bucket", "confirmation"},
					Controller: "HandleStartBatchJob",
					Response:   batchdto.Job{}, Handler: func(app *runtime.
							State,
						w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleStartBatchJob(app, w, r, s)
					},
				},
				{Suffix: "{id}", Method: "GET", Summary: "Пакетные задания",
					Input:      routes.Inputs(),
					Controller: "HandleBatchJobStatus",
					Response:   batchdto.BatchMetric{}, Handler: func(app *runtime.
							State,
						w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleBatchJobStatus(app, w, r, s, rest)
					},
				},
				{Suffix: "{id}", Method: "DELETE", Mutation: routes.AdminLock, Summary: "Пакетные задания",
					Input:      routes.Inputs(),
					Request:    httpvalidation.ConfirmationRequest{},
					Required:   []string{"confirmation"},
					Controller: "HandleCancelBatchJob",
					Response:   resultdto.Success{}, Handler: func(app *runtime.
							State,
						w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleCancelBatchJob(app, w, r, s, rest)
					},
				},
				{Suffix: "{id}/parameters", Method: "GET", Summary: "Пакетные задания",
					Input:      routes.Inputs(),
					Controller: "HandleBatchParameters",
					Response:   batchdto.BatchParameters{}, Handler: func(app *runtime.
							State,
						w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleBatchParameters(app, w, r, s, strings.TrimSuffix(rest, "/parameters"))
					},
				},
			},
		},
	}
}
