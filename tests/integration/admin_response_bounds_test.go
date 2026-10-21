package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/minio/madmin-go/v3"

	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	servercontroller "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/server/controllers"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/presentation"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	serversvc "github.com/owiks/golang-minio-s3-backend-api/src/services/server"
)

func TestAdminResponseBoundsProtectConfigReadsAndRestoreBeforeWrite(t *testing.T) {
	for _, source := range []string{"configuration", "history"} {
		for _, method := range []string{"GET", "PUT"} {
			t.Run(source+"/"+method, func(t *testing.T) {
				normal, err := madmin.EncryptData("public-fixture-secret", []byte("api requests_max=0"))
				if err != nil {
					t.Fatal(err)
				}
				var oversized atomic.Bool
				var writes, configReads, historyReads atomic.Int32
				s := actionCheckSession(t, func(w http.ResponseWriter, r *http.Request) {
					switch {
					case strings.HasSuffix(r.URL.Path, "/accountinfo"):
						httpcommon.WriteJSON(w, r, 200, map[string]any{"AccountName": "fixture-user", "Policy": actionCheckPolicy})
					case strings.HasSuffix(r.URL.Path, "/get-config-kv"):
						configReads.Add(1)
						if oversized.Load() && source == "configuration" {
							// Unknown/chunked length, deliberately not decryptable. A
							// size error proves rejection before the SDK sees payload.
							w.(http.Flusher).Flush()
							_, _ = w.Write(bytes.Repeat([]byte("x"), 1<<20))
						} else {
							_, _ = w.Write(normal)
						}
					case strings.HasSuffix(r.URL.Path, "/list-config-history-kv"):
						historyReads.Add(1)
						w.(http.Flusher).Flush()
						_, _ = w.Write(bytes.Repeat([]byte("x"), 5<<20))
					default:
						writes.Add(1)
						httpcommon.WriteJSON(w, r, 200, map[string]bool{"ok": true})
					}
				})
				current, err := serversvc.ReadServerConfig(context.Background(), runtimesvc.NewClients(s), "api")
				if err != nil {
					t.Fatal(err)
				}
				configReads.Store(0)
				oversized.Store(true)
				payload, _ := json.Marshal(serversvc.ConfigChangeRequest{Revision: current.Revision, RestoreID: "entry"})
				r := httptest.NewRequest(method, "/?subsystem=api", bytes.NewReader(payload))
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				app := runtimesvc.New(testConfig())
				defer app.Close()
				if method == "GET" {
					err = servercontroller.HandleReadServerConfig(app, w, r, s)
				} else {
					err = servercontroller.HandleSaveServerConfig(app, w, r, s)
				}
				if source == "history" && method == "GET" {
					var view presentation.ServerConfigView
					if err != nil || json.Unmarshal(w.Body.Bytes(), &view) != nil || view.HistoryError == "" || len(view.History) != 0 {
						t.Fatal("optional history error hid current config or accepted history", err)
					}
				} else if err == nil || apperror.Public(err).Status != 502 || apperror.Public(err).Code != "AdminResponseTooLarge" {
					t.Fatal("size error lost at HTTP/service boundary", err)
				}
				if writes.Load() != 0 || configReads.Load() != 1 || historyReads.Load() > 1 {
					t.Fatal("retried oversized read or attempted mutation", writes.Load(), configReads.Load(), historyReads.Load())
				}
			})
		}
	}
}
