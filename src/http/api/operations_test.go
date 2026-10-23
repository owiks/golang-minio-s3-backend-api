package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	operationcontrollers "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/operations/controllers"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/operations"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/tests/testsession"
)

const transferOperationBody = `{"items":[{"sourceBucket":"data","sourceKey":"reports/file.txt","sourcePrefix":"reports/","sourceRevision":"source-revision","destinationBucket":"archive","destinationKey":"copy/file.txt","destinationPrefix":"copy/","destinationRevision":"missing"}]}`

type operationsFixture struct {
	t          *testing.T
	app        *runtime.State
	handler    http.Handler
	ctx        context.Context
	root       authstate.Access
	connection authstate.Connection
	token      authstate.Issued
}

func newOperationsFixture(t *testing.T, cfg config.Config, policy authstate.Policy) *operationsFixture {
	t.Helper()
	const endpoint = "http://127.0.0.1:19000"
	cfg.Endpoints.Allowed[endpoint] = true
	app := runtime.New(cfg)
	t.Cleanup(app.Close)
	s := testsession.New(t, endpoint)
	if err := app.StoreSession(s); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	a, c, err := app.Auth.Lookup(ctx, s.SessionID())
	if err != nil {
		t.Fatal(err)
	}
	issued, err := app.Auth.Issue(ctx, authstate.CredentialGuard(a, false), c.Snapshot().ID, authstate.IssueRequest{TTLSeconds: 600, Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	return &operationsFixture{t: t, app: app, handler: NewHandler(app), ctx: ctx, root: a, connection: c, token: issued}
}
func (f *operationsFixture) call(method, path, body string, want int, headers ...string) *httptest.ResponseRecorder {
	f.t.Helper()
	r := httptest.NewRequest(method, "/api/sessions/"+f.token.ID+"/operations"+path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+f.token.Token)
	r.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Add(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != want {
		f.t.Fatalf("%s %s: got %d want %d: %s", method, path, w.Code, want, w.Body.String())
	}
	return w
}
func operationView(t *testing.T, w *httptest.ResponseRecorder) operationcontrollers.View {
	t.Helper()
	var v operationcontrollers.View
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func (f *operationsFixture) scope() operations.Scope {
	return operations.Scope{ActorID: f.token.ID, ConnectionID: f.connection.Snapshot().ID}
}

func TestOperationsSubmissionIdempotencyAndOwnership(t *testing.T) {
	f := newOperationsFixture(t, config.Default(), authstate.FullPolicy())
	w := f.call("POST", "/transfers", transferOperationBody, 202, "Idempotency-Key", "submit-batch")
	created := operationView(t, w)
	if created.ID == "" || created.State != operations.Queued || created.Total != 1 || created.Revision != 1 || created.Kind != "transfer" {
		t.Fatal("unexpected submitted operation", created)
	}
	if len(created.Display.Items) != 1 || created.Display.Items[0].SourceBucket != "data" || created.Display.Items[0].SourceKey != "reports/file.txt" || created.Display.Items[0].DestinationBucket != "archive" || created.Display.Items[0].DestinationKey != "copy/file.txt" {
		t.Fatal("submitted transfer cannot restore its display", created.Display)
	}
	for _, language := range []string{"en", "ru"} {
		read := f.call("GET", "/"+created.ID, "", 200, "Accept-Language", language)
		if got := operationView(t, read); len(got.Display.Items) != 1 || got.Display.Items[0] != created.Display.Items[0] {
			t.Fatal("operation display depends on response language", language, got.Display)
		}
		for _, hidden := range []string{"source-revision", `"sourceRevision"`, `"destinationRevision"`, `"sourcePrefix"`, `"destinationPrefix"`} {
			if strings.Contains(read.Body.String(), hidden) {
				t.Fatal("operation display exposed conditional-write manifest", hidden)
			}
		}
	}
	location := "/api/sessions/" + f.token.ID + "/operations/" + created.ID
	if w.Header().Get("Location") != location || w.Header().Get("X-Command-ID") == "" {
		t.Fatal("missing operation location/command ID", w.Header())
	}
	w = f.call("POST", "/transfers", transferOperationBody, 202, "Idempotency-Key", "submit-batch")
	if next := operationView(t, w); next.ID != created.ID || w.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal("submission replay inserted a new operation", next, w.Header())
	}
	changed := strings.Replace(transferOperationBody, "source-revision", "changed-revision", 1)
	f.call("POST", "/transfers", changed, 409, "Idempotency-Key", "submit-batch")
	f.call("GET", "/"+created.ID, "", 200)
	w = f.call("GET", "", "", 200)
	var list []operationcontrollers.View
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list) != 1 || list[0].ID != created.ID {
		t.Fatal("list/replay mismatch", list, err)
	}
	other, err := f.app.Auth.Issue(f.ctx, authstate.CredentialGuard(f.root, false), f.connection.Snapshot().ID, authstate.IssueRequest{TTLSeconds: 600, Policy: authstate.FullPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	f.token = other
	f.call("GET", "/"+created.ID, "", 404)
	f.call("POST", "/"+created.ID+"/cancel", `{"revision":1}`, 404)
	f.call("POST", "/"+created.ID+"/retry", `{"revision":1}`, 404)
	if w = f.call("GET", "", "", 200); strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatal("another access can list the operation", w.Body.String())
	}
}

func TestOperationsHideExecutionAndContinuationState(t *testing.T) {
	f := newOperationsFixture(t, config.Default(), authstate.Policy{Scopes: []string{"objects:read"}, Resources: []authstate.Resource{{Bucket: "data", Prefix: "reports/"}}})
	w := f.call("POST", "/scans", `{"bucket":"data","prefix":"reports/"}`, 202)
	created := operationView(t, w)
	now := time.Now()
	claimed, err := f.app.Operations.Claim(f.ctx, "secret-worker-identity", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := f.app.Operations.Save(f.ctx, created.ID, claimed.LeaseID, claimed.Revision, operations.Checkpoint{Page: &operations.ScanPage{Objects: 2, Bytes: 123, NextToken: "secret-continuation-token"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err = f.app.Operations.Claim(f.ctx, "secret-worker-identity", now.Add(time.Microsecond), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", "/" + created.ID} {
		w = f.call("GET", path, "", 200)
		for _, hidden := range []string{"scope", "spec", "actorID", "connectionID", "deadline", "workerID", "leaseID", "leaseUntil", "lastServedAt", "inFlight", "nextIndex", "token", "seen", "secret-worker-identity", "secret-continuation-token", claimed.LeaseID, checkpoint.Scan.Seen[0]} {
			if strings.Contains(w.Body.String(), hidden) {
				t.Fatal("operation response exposed execution state", hidden, w.Body.String())
			}
		}
	}
	v := operationView(t, w)
	if v.Scan == nil || v.Scan.Objects != 2 || v.Scan.Bytes != 123 || v.Scan.Pages != 1 || v.State != operations.Running || v.Display.Bucket != "data" || v.Display.Prefix != "reports/" {
		t.Fatal("scan progress missing", v)
	}
	f.call("POST", "/scans", `{"bucket":"data","prefix":"outside/"}`, 403)
}

func TestOperationsNarrowingAllowsCancellationButNotReadOrRetry(t *testing.T) {
	f := newOperationsFixture(t, config.Default(), authstate.FullPolicy())
	created := operationView(t, f.call("POST", "/transfers", transferOperationBody, 202))
	narrow := authstate.Policy{Scopes: []string{"objects:read"}, Resources: []authstate.Resource{{Bucket: "data", Prefix: "reports/"}}}
	if _, err := f.app.Auth.Edit(f.ctx, authstate.CredentialGuard(f.root, false), f.token.ID, authstate.EditRequest{Revision: 1, Policy: &narrow}); err != nil {
		t.Fatal(err)
	}
	f.call("GET", "/"+created.ID, "", 403)
	if w := f.call("GET", "", "", 200); strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatal("narrowed access can list forbidden resources", w.Body.String())
	}
	f.call("POST", "/"+created.ID+"/retry", `{"revision":1}`, 403)
	w := f.call("POST", "/"+created.ID+"/cancel", `{"revision":1}`, 200)
	if strings.TrimSpace(w.Body.String()) != `{"ok":true}` {
		t.Fatal("ownership cancellation exposed operation metadata", w.Body.String())
	}
	saved, err := f.app.Operations.Get(f.ctx, f.scope(), created.ID)
	if err != nil || saved.State != operations.Cancelled {
		t.Fatal("cancel did not persist", saved, err)
	}
}

func TestOperationsValidationRetryAndOriginalDeadline(t *testing.T) {
	f := newOperationsFixture(t, config.Default(), authstate.FullPolicy())
	for _, body := range []string{`{}`, `{"items":[]}`, `{"items":[],"extra":true}`, `{"items":null}`} {
		f.call("POST", "/transfers", body, 400)
	}
	f.call("POST", "/transfers", transferOperationBody, 400, "Idempotency-Key", "")
	f.call("POST", "/transfers", transferOperationBody, 400, "Idempotency-Key", "first", "Idempotency-Key", "second")
	f.call("POST", "/scans", `{"bucket":"data","prefix":"","extra":true}`, 400)
	created := operationView(t, f.call("POST", "/scans", `{"bucket":"data","prefix":""}`, 202))
	f.call("POST", "/"+created.ID+"/cancel", `{}`, 400)
	f.call("POST", "/"+created.ID+"/cancel", `{"revision":0}`, 400)
	f.call("POST", "/"+created.ID+"/cancel", `{"revision":-1}`, 400)
	f.call("POST", "/"+created.ID+"/cancel", `{"revision":2}`, 409)
	claimed, err := f.app.Operations.Claim(f.ctx, "fixture-worker", time.Now(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := f.app.Operations.Save(f.ctx, created.ID, claimed.LeaseID, claimed.Revision, operations.Checkpoint{FailCode: "ScanFailed"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	w := f.call("POST", "/"+created.ID+"/retry", fmt.Sprintf(`{"revision":%d}`, failed.Revision), 200)
	if v := operationView(t, w); v.State != operations.Queued {
		t.Fatal("known failure cannot be retried", v)
	}
	after, err := f.app.Operations.Get(f.ctx, f.scope(), created.ID)
	if err != nil || !after.Deadline.Equal(failed.Deadline) {
		t.Fatal("retry changed the authorization deadline", err)
	}
	now := time.Now()
	expired, err := f.app.Operations.Insert(f.ctx, f.scope(), operations.Spec{Scan: &operations.ScanSpec{Bucket: "data"}}, now.Add(-time.Minute), now.Add(-2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	// The expired record sorts before the current job; claiming expires it before
	// choosing any runnable work, without sleeping or changing the real clock.
	if _, err = f.app.Operations.Claim(f.ctx, "fixture-worker", now, time.Minute); err != nil {
		t.Fatal(err)
	}
	expired, err = f.app.Operations.Get(f.ctx, f.scope(), expired.ID)
	if err != nil || expired.State != operations.Failed {
		t.Fatal("expired queued operation was not finalized", expired, err)
	}
	f.call("POST", "/"+expired.ID+"/retry", fmt.Sprintf(`{"revision":%d}`, expired.Revision), 409)
}

func TestOperationsMaintenanceKeepsReadsScansAndCancellation(t *testing.T) {
	cfg := config.Default()
	cfg.Operations.ReadOnly = true
	f := newOperationsFixture(t, cfg, authstate.FullPolicy())
	var request operationcontrollers.TransfersRequest
	if err := json.Unmarshal([]byte(transferOperationBody), &request); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	saved, err := f.app.Operations.Insert(f.ctx, f.scope(), operations.Spec{Transfers: request.Items}, now.Add(time.Minute), now)
	if err != nil {
		t.Fatal(err)
	}
	f.call("GET", "/"+saved.ID, "", 200)
	f.call("GET", "", "", 200)
	f.call("POST", "/"+saved.ID+"/cancel", fmt.Sprintf(`{"revision":%d}`, saved.Revision), 200)
	f.call("POST", "/transfers", transferOperationBody, 503)
	f.call("POST", "/scans", `{"bucket":"data","prefix":""}`, 202)
}

func TestOperationsTransferChecksDestinationAndMovePermission(t *testing.T) {
	readWrite := authstate.Policy{Scopes: []string{"objects:read", "objects:write"}, Resources: []authstate.Resource{{Bucket: "data", Prefix: "reports/"}, {Bucket: "archive", Prefix: "copy/"}}}
	f := newOperationsFixture(t, config.Default(), readWrite)
	f.call("POST", "/transfers", transferOperationBody, 202)
	move := strings.Replace(transferOperationBody, `"sourceBucket"`, `"move":true,"sourceBucket"`, 1)
	f.call("POST", "/transfers", move, 403)
	outside := strings.ReplaceAll(transferOperationBody, "copy/", "outside/")
	f.call("POST", "/transfers", outside, 403)
}
