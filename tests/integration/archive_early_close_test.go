package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	archivescontroller "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/archives/controllers"
	archiveplan "github.com/owiks/golang-minio-s3-backend-api/src/services/archives/plan"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/tests/testsession"
)

type archiveSetupFailureWriter struct{ *httptest.ResponseRecorder }

func (*archiveSetupFailureWriter) Header() http.Header { panic("archive response setup failed") }

func TestArchiveEarlyCloseAfterResponseSetupFailure(t *testing.T) {
	app := runtimesvc.New(testConfig())
	t.Cleanup(app.Close)
	session := testsession.New(t, "http://fixture.example")
	if err := app.StoreSession(session); err != nil {
		t.Fatal(err)
	}
	plan := archiveplan.New(archiveplan.Config{SessionID: session.SessionID(), Bucket: "bucket", Created: time.Now(), Name: "test.zip", Items: []archiveplan.ArchiveItem{{Key: "file", Path: "file", State: "pending"}}})
	ticket, err := app.Archives.Put(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if value := recover(); value != "archive response setup failed" {
				t.Fatal("response failure was not preserved", value)
			}
		}()
		writer := &archiveSetupFailureWriter{httptest.NewRecorder()}
		if err := archivescontroller.HandleDownloadArchive(app, writer, httptest.NewRequest("GET", "/", nil), session, ticket); err != nil {
			t.Fatal(err)
		}
	}()
	if snapshot := plan.Snapshot(ticket); !snapshot.Started || !snapshot.Finished || !snapshot.Cancelled || snapshot.Items[0].State != "cancelled" {
		t.Fatal("response setup failure stranded a running archive", snapshot)
	}
	if err := archivescontroller.HandleForgetArchive(app, httptest.NewRecorder(), httptest.NewRequest("DELETE", "/", nil), session, ticket); err != nil || app.Archives.Get(ticket, session.SessionID()) != nil {
		t.Fatal("failed download cannot be forgotten through HTTP", err)
	}
}
