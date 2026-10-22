package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	operationcontrollers "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/operations/controllers"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/operations"
)

func TestOperationPauseIsVisibleWithoutLeakingSiblingJobs(t *testing.T) {
	cfg := config.Default()
	cfg.OperatorToken = strings.Repeat("operator-secret-", 4)
	f := newOperationsFixture(t, cfg, authstate.FullPolicy())
	scan := operationView(t, f.call("POST", "/scans", `{"bucket":"data","prefix":"reports/"}`, 202))
	now := time.Now()
	claimed, err := f.app.Operations.Claim(f.ctx, "private-worker", now, time.Minute)
	if err != nil || claimed.ID != scan.ID {
		t.Fatal("cannot claim scan", err)
	}
	paused, err := f.app.Operations.Save(f.ctx, claimed.ID, claimed.LeaseID, claimed.Revision, operations.Checkpoint{Throttle: true}, now)
	if err != nil {
		t.Fatal("cannot persist upstream throttle", err)
	}
	transfer := operationView(t, f.call("POST", "/transfers", transferOperationBody, 202))
	for _, path := range []string{"/" + scan.ID, "/" + transfer.ID} {
		w := f.call("GET", path, "", 200)
		v := operationView(t, w)
		if v.Phase != "waiting" || v.WaitingReason != "upstream_throttled" || v.RetryAt == nil || !v.RetryAt.Equal(paused.NotBefore) {
			t.Fatal("shared pause missing", v)
		}
		for _, private := range []string{"private-worker", claimed.LeaseID, "notBefore", "throttleTotal", "throttleConsecutive"} {
			if strings.Contains(w.Body.String(), private) {
				t.Fatal("private scheduler state leaked", private)
			}
		}
	}
	var list []operationcontrollers.View
	if err := json.Unmarshal(f.call("GET", "", "", 200).Body.Bytes(), &list); err != nil || len(list) != 2 {
		t.Fatal("invalid paused operation list", err)
	}
	for _, v := range list {
		if v.WaitingReason != "upstream_throttled" || v.RetryAt == nil {
			t.Fatal("list omitted shared pause", v)
		}
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/operator/operations", nil)
	r.Header.Set("Authorization", "Bearer "+cfg.OperatorToken)
	f.handler.ServeHTTP(w, r)
	var status operations.OperatorStatus
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &status) != nil || !operations.ValidStatus(status) || status.Queue.Queued != 2 || status.Queue.Throttled != 2 {
		t.Fatal("operator summary omitted delayed work", w.Code, w.Body.String())
	}
	f.call("POST", "/"+scan.ID+"/cancel", `{"revision":`+strconv.FormatUint(paused.Revision, 10)+`}`, 200)
	if cancelled := operationView(t, f.call("GET", "/"+scan.ID, "", 200)); cancelled.State != operations.Cancelled || cancelled.RetryAt != nil {
		t.Fatal("cancelled operation still appears runnable", cancelled)
	}
	if waiting := operationView(t, f.call("GET", "/"+transfer.ID, "", 200)); waiting.WaitingReason != "upstream_throttled" || waiting.RetryAt == nil {
		t.Fatal("cancel lifted the shared pause", waiting)
	}
}
