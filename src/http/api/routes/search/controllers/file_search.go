package controllers

import (
	"net/http"

	httpcommon "minio-web/api/src/http/api/common"
	"minio-web/api/src/services/apperror"
	runtimesvc "minio-web/api/src/services/runtime"
	searchsvc "minio-web/api/src/services/search"
)

func HandleFileSearch(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	if r.Method != "GET" {
		return apperror.New(405, "MethodNotAllowed", "Метод не поддерживается.")
	}
	q := r.URL.Query()
	value, err := searchsvc.SearchFiles(r.Context(), runtimesvc.NewClients(s), searchsvc.FileSearchInput{
		Bucket: q.Get("bucket"), Prefix: q.Get("prefix"), Token: q.Get("token"),
		Query: q.Get("q"), Extensions: q.Get("extensions"),
		MinSize: q.Get("minSize"), MaxSize: q.Get("maxSize"),
		After: q.Get("after"), Before: q.Get("before"),
	})
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, 200, value)
	return nil
}
