package buckets

import (
	"net/http"
	"strings"

	"minio-web/api/src/http/api/routes"
	"minio-web/api/src/http/api/routes/buckets/controllers"
	httpvalidation "minio-web/api/src/http/validation"
	"minio-web/api/src/services/apperror"
	bucketsdto "minio-web/api/src/services/buckets"
	bucketssvc "minio-web/api/src/services/buckets"
	"minio-web/api/src/services/messages"
	objectsdto "minio-web/api/src/services/objects"
	resultdto "minio-web/api/src/services/result"
	"minio-web/api/src/services/runtime"
)

func GetRoutes() []routes.APIRoute {
	return []routes.APIRoute{
		{
			Path: "admin/bucket-usage",
			Tag:  "buckets",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Статистика бакетов",
					Input:      routes.Inputs(),
					Controller: "HandleBucketUsage",
					Response:   bucketsdto.UsageReply{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						if r.Method != "GET" {
							return apperror.New(405, "MethodNotAllowed", messages.ManagementUnsupported)
						}
						return controllers.HandleBucketUsage(app, w, r, s)
					}},
			},
		},
		{
			Path: "admin/buckets",
			Tag:  "buckets",
			Operations: []routes.Operation{
				{Suffix: "", Method: "GET", Summary: "Бакеты",
					Input:      routes.Inputs(),
					Controller: "HandleListBuckets",
					Response:   []objectsdto.BucketRow{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleListBuckets(app, w, r, s)
					}},
				{Suffix: "", Method: "POST", Mutation: routes.AfterDecode, Summary: "Бакеты",
					Input:      routes.Inputs(),
					Controller: "HandleCreateBucket",
					Request:    bucketssvc.CreateBucketRequest{},
					Response:   resultdto.Success{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleCreateBucket(app, w, r, s)
					}},
				{Suffix: "{name}", Method: "DELETE", Mutation: routes.BucketLock, Summary: "Бакеты",
					Input:      routes.Inputs(),
					Request:    httpvalidation.ConfirmationRequest{},
					Required:   []string{"confirmation"},
					Controller: "HandleDeleteBucket",
					Response:   resultdto.Success{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleDeleteBucket(app, w, r, s, rest)
					}},
				{Suffix: "{name}/details", Method: "GET", Summary: "Бакеты",
					Input:      routes.Inputs(routes.Query("fields", false, 128).Describe("Необязательный список: quota,tags,versioning,locking,policy,lifecycle,cors,encryption; через запятую.")),
					Controller: "HandleBucketDetails",
					Response:   bucketsdto.BucketDetails{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleBucketDetails(app, w, r, s, strings.TrimSuffix(rest, "/details"))
					}},
				{Suffix: "{name}/access", Method: "GET", Summary: "Бакеты",
					Input:      routes.Inputs(),
					Controller: "HandleBucketAccess",
					Response:   bucketsdto.BucketAccessReport{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleBucketAccess(app, w, r, s, strings.TrimSuffix(rest, "/access"))
					}},
				{Suffix: "{name}/lifecycle", Method: "GET", Summary: "Бакеты",
					Input:      routes.Inputs(),
					Controller: "HandleReadBucketLifecycle",
					Response:   bucketsdto.DetailField{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleReadBucketLifecycle(app, w, r, s, strings.TrimSuffix(rest, "/lifecycle"))
					}},
				{Suffix: "{name}/lifecycle", Method: "POST", Mutation: routes.BucketLock, Summary: "Бакеты",
					Input:      routes.Inputs(),
					Request:    bucketsdto.LifecycleChangeRequest{},
					Required:   []string{"revision", "rule"},
					Controller: "HandleChangeBucketLifecycle",
					Response:   bucketsdto.DetailField{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleChangeBucketLifecycle(app, w, r, s, strings.TrimSuffix(rest, "/lifecycle"), "create")
					}},
				{Suffix: "{name}/lifecycle", Method: "PUT", Mutation: routes.BucketLock, Summary: "Бакеты",
					Input:      routes.Inputs(),
					Request:    bucketsdto.LifecycleChangeRequest{},
					Required:   []string{"revision", "index", "rule"},
					Controller: "HandleChangeBucketLifecycle",
					Response:   bucketsdto.DetailField{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleChangeBucketLifecycle(app, w, r, s, strings.TrimSuffix(rest, "/lifecycle"), "update")
					}},
				{Suffix: "{name}/lifecycle", Method: "PATCH", Mutation: routes.BucketLock, Summary: "Бакеты",
					Input:      routes.Inputs(),
					Request:    bucketsdto.LifecycleChangeRequest{},
					Required:   []string{"revision", "index", "enabled"},
					Controller: "HandleChangeBucketLifecycle",
					Response:   bucketsdto.DetailField{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleChangeBucketLifecycle(app, w, r, s, strings.TrimSuffix(rest, "/lifecycle"), "toggle")
					}},
				{Suffix: "{name}/lifecycle", Method: "DELETE", Mutation: routes.BucketLock, Summary: "Бакеты",
					Input:      routes.Inputs(),
					Request:    bucketsdto.LifecycleChangeRequest{},
					Required:   []string{"revision", "index", "confirmation"},
					Controller: "HandleChangeBucketLifecycle",
					Response:   bucketsdto.DetailField{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleChangeBucketLifecycle(app, w, r, s, strings.TrimSuffix(rest, "/lifecycle"), "delete")
					}},
				{Suffix: "{name}/versioning", Method: "PUT", Mutation: routes.BucketLock, Summary: "Бакеты",
					Input:      routes.Inputs(routes.Query("fields", false, 128).Describe("Необязательный список: quota,tags,versioning,locking,policy,lifecycle,cors,encryption; через запятую.")),
					Controller: "HandleUpdateBucketRetention",
					Request:    bucketssvc.UpdateBucketVersioningRequest{},
					Response:   bucketsdto.BucketDetails{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleUpdateBucketRetention(app, w, r, s, strings.TrimSuffix(rest, "/versioning"), "versioning")
					}},
				{Suffix: "{name}/locking", Method: "PUT", Mutation: routes.BucketLock, Summary: "Бакеты",
					Input:      routes.Inputs(routes.Query("fields", false, 128).Describe("Необязательный список: quota,tags,versioning,locking,policy,lifecycle,cors,encryption; через запятую.")),
					Controller: "HandleUpdateBucketRetention",
					Request:    bucketssvc.UpdateBucketLockingRequest{},
					Response:   bucketsdto.BucketDetails{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleUpdateBucketRetention(app, w, r, s, strings.TrimSuffix(rest, "/locking"), "locking")
					}},
				{Suffix: "{name}/policy", Method: "PUT", Mutation: routes.BucketLock, Summary: "Бакеты",
					Input:      routes.Inputs(),
					Controller: "HandleUpdateBucketSetting",
					Request:    bucketssvc.UpdateBucketPolicyRequest{},
					Response:   bucketsdto.DetailField{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleUpdateBucketSetting(app, w, r, s, strings.TrimSuffix(rest, "/policy"), "policy")
					}},
				{Suffix: "{name}/quota", Method: "PUT", Mutation: routes.BucketLock, Summary: "Бакеты",
					Input:      routes.Inputs(),
					Controller: "HandleUpdateBucketSetting",
					Request:    bucketssvc.UpdateBucketQuotaRequest{},
					Response:   bucketsdto.DetailField{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleUpdateBucketSetting(app, w, r, s, strings.TrimSuffix(rest, "/quota"), "quota")
					}},
				{Suffix: "{name}/tags", Method: "PUT", Mutation: routes.BucketLock, Summary: "Бакеты",
					Input:      routes.Inputs(),
					Controller: "HandleUpdateBucketSetting",
					Request:    bucketssvc.UpdateBucketTagsRequest{},
					Response:   bucketsdto.DetailField{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleUpdateBucketSetting(app, w, r, s, strings.TrimSuffix(rest, "/tags"), "tags")
					}},
				{Suffix: "{name}/cors", Method: "GET", Summary: "Бакеты",
					Input:      routes.Inputs(),
					Controller: "HandleReadBucketBrowserSetting",
					Response:   bucketsdto.DetailField{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleReadBucketBrowserSetting(app, w, r, s, strings.TrimSuffix(rest, "/cors"), "cors")
					}},
				{Suffix: "{name}/cors", Method: "POST", Mutation: routes.BucketLock, Summary: "Бакеты",
					Input:      routes.Inputs(),
					Request:    bucketsdto.BrowserChangeRequest{},
					Required:   []string{"revision", "rule"},
					Controller: "HandleChangeBucketBrowserSetting",
					Response:   bucketsdto.DetailField{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleChangeBucketBrowserSetting(app, w, r, s, strings.TrimSuffix(rest, "/cors"), "cors", "create")
					}},
				{Suffix: "{name}/cors", Method: "PUT", Mutation: routes.BucketLock, Summary: "Бакеты",
					Input:      routes.Inputs(),
					Request:    bucketsdto.BrowserChangeRequest{},
					Required:   []string{"revision", "index", "rule"},
					Controller: "HandleChangeBucketBrowserSetting",
					Response:   bucketsdto.DetailField{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleChangeBucketBrowserSetting(app, w, r, s, strings.TrimSuffix(rest, "/cors"), "cors", "update")
					}},
				{Suffix: "{name}/cors", Method: "DELETE", Mutation: routes.BucketLock, Summary: "Бакеты",
					Input:      routes.Inputs(),
					Request:    bucketsdto.BrowserChangeRequest{},
					Required:   []string{"revision", "index", "confirmation"},
					Controller: "HandleChangeBucketBrowserSetting",
					Response:   bucketsdto.DetailField{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleChangeBucketBrowserSetting(app, w, r, s, strings.TrimSuffix(rest, "/cors"), "cors", "delete")
					}},
				{Suffix: "{name}/encryption", Method: "GET", Summary: "Бакеты",
					Input:      routes.Inputs(),
					Controller: "HandleReadBucketBrowserSetting",
					Response:   bucketsdto.DetailField{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleReadBucketBrowserSetting(app, w, r, s, strings.TrimSuffix(rest, "/encryption"), "encryption")
					}},
				{Suffix: "{name}/encryption", Method: "PUT", Mutation: routes.BucketLock, Summary: "Бакеты",
					Input:      routes.Inputs(),
					Request:    bucketsdto.BrowserChangeRequest{},
					Required:   []string{"revision"},
					Controller: "HandleChangeBucketBrowserSetting",
					Response:   bucketsdto.DetailField{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleChangeBucketBrowserSetting(app, w, r, s, strings.TrimSuffix(rest, "/encryption"), "encryption", "update")
					}},
			},
		},
	}
}
