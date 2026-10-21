package integration

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"

	httpcommon "minio-web/api/src/http/api/common"
	archivescontroller "minio-web/api/src/http/api/routes/archives/controllers"
	"minio-web/api/src/services/apperror"
	archivessvc "minio-web/api/src/services/archives"
	archiveplan "minio-web/api/src/services/archives/plan"
	"minio-web/api/src/services/messages"
	objectstate "minio-web/api/src/services/objects/state"
	runtimesvc "minio-web/api/src/services/runtime"
	"minio-web/api/tests/testsession"
)

func TestArchiveNamesAndPaths(t *testing.T) {
	for _, name := range []string{"../archive", "a:b.zip", ".zip", "a\n.zip", strings.Repeat("я", 100)} {
		if _, err := archiveplan.ArchiveFilename(name); err == nil {
			t.Fatal("accepted", name)
		}
	}
	if name, err := archiveplan.ArchiveFilename("Отчёт"); err != nil || name != "Отчёт.zip" {
		t.Fatal(name, err)
	}
	items := []archiveplan.ArchiveItem{{Key: "root/a/report.txt"}, {Key: "root/b/report.txt"}}
	paths, err := archiveplan.ArchivePaths(items, "root/")
	if err != nil || paths[0] != "a/report.txt" || paths[1] != "b/report.txt" {
		t.Fatal(paths, err)
	}
	if _, err = archiveplan.ArchivePaths(items, "roo"); err == nil {
		t.Fatal("partial segment accepted")
	}
	if _, err = archiveplan.ArchivePaths(items, "other/"); err == nil {
		t.Fatal("outside scope accepted")
	}
}
func TestArchiveControlAndSnapshotOwnership(t *testing.T) {
	app := runtimesvc.New(testConfig())
	session := testsession.New(t, "http://fixture.example")
	plan := archiveplan.New(archiveplan.Config{SessionID: session.SessionID(), Name: "a.zip", Created: time.Now(), RelativeBase: "root/", Base: "root/", Items: []archiveplan.ArchiveItem{{Key: "root/a", Path: "a", State: "pending", Size: 5, Info: objectstate.DeletionSnapshot{ETag: "x"}}}, Issues: map[string]messages.Ref{}})
	if err := app.StoreSession(session); err != nil {
		t.Fatal(err)
	}
	ticket, err := app.Archives.Put(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, body string, statusOnly bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://ui.example", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		path := "objects/archive/" + ticket
		if statusOnly {
			path = "objects/archive-status/" + ticket
		}
		if err := runHTTPAction(app, w, r, session, path); err != nil {
			httpcommon.WriteJSON(w, r, apperror.Public(err).Status, apperror.Public(err))
		}
		return w
	}
	if w := call("PUT", `{"name":"Новый","base":"","allowPartial":true}`, false); w.Code != 200 || plan.Snapshot(ticket).Name != "Новый.zip" || plan.Snapshot(ticket).Items[0].Path != "root/a" {
		t.Fatal(w.Code, w.Body.String())
	}
	snapshot := plan.Snapshot(ticket)
	snapshot.Items[0].State = "tampered"
	if plan.Snapshot(ticket).Items[0].State != "pending" {
		t.Fatal("snapshot aliases live state")
	}
	readContext, _, cancel, err := plan.BeginDownload(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if w := call("DELETE", "", true); w.Code != 409 {
		t.Fatal(w.Code)
	}
	if w := call("DELETE", "", false); w.Code != 200 || readContext.Err() != context.Canceled || !plan.Snapshot(ticket).Cancelled {
		t.Fatal(w.Code, readContext.Err())
	}
	plan.Finish(readContext.Err(), false)
	// A separately completed plan must not be marked cancelled by a late stop.
	plan = archiveplan.New(archiveplan.Config{SessionID: session.SessionID(), Created: time.Now(), Items: []archiveplan.ArchiveItem{{Key: "file", State: "pending"}}})
	_, _, completedCancel, err := plan.BeginDownload(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer completedCancel()
	plan.CompleteItem(0)
	plan.Finish(nil, true)
	ticket, err = app.Archives.Put(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if w := call("DELETE", "", false); w.Code != 200 || plan.Snapshot(ticket).Cancelled {
		t.Fatal("stop raced with successful completion", w.Code)
	}
	if w := call("DELETE", "", true); w.Code != 200 || app.Archives.Get(ticket, session.SessionID()) != nil {
		t.Fatal(w.Code)
	}
}
func TestArchivePanelIntegration(t *testing.T) {
	c, s3, b := fileFixture(t)
	ctx := context.Background()
	for _, key := range []string{"tree/a.txt", "tree/b.txt", "tree/nested/c.txt"} {
		if _, err := s3.PutObject(ctx, b, key, strings.NewReader("content"), 7, minio.PutObjectOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	var plan struct {
		Ticket, Name string
		Count        int
		Items        []archiveplan.ArchiveItem
		Issues       map[string]string
		Sent         int64
		Finished     bool
	}
	body := map[string]any{"bucket": b, "name": "Отчёт.zip", "base": "tree/", "entries": []archivessvc.ArchiveSelection{{Key: "tree/", Kind: "folder"}, {Key: "tree/nested/", Kind: "folder"}, {Key: "tree/a.txt", Kind: "file"}}}
	_ = json.Unmarshal(c.call("POST", "objects/archive", body, 200), &plan)
	if plan.Count != 3 || plan.Items[0].Path != "a.txt" {
		t.Fatal(plan)
	}
	c.call("PUT", "objects/archive/"+plan.Ticket, map[string]any{"name": "Отчёт.zip", "base": "tree/"}, 200)
	_ = s3.RemoveObject(ctx, b, "tree/b.txt", minio.RemoveObjectOptions{})
	raw := c.call("GET", "objects/archive/"+plan.Ticket, nil, 200)
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, file := range reader.File {
		names[file.Name] = true
	}
	if !names["a.txt"] || !names["nested/c.txt"] || names["tree/a.txt"] || names["b.txt"] {
		t.Fatal(names)
	}
	_ = json.Unmarshal(c.call("GET", "objects/archive-status/"+plan.Ticket, nil, 200), &plan)
	if !plan.Finished || plan.Sent != 14 || plan.Items[1].State != "error" {
		t.Fatal(plan)
	}
	c.call("GET", "objects/archive/"+plan.Ticket, nil, 409)
	c.call("DELETE", "objects/archive-status/"+plan.Ticket, nil, 200)
	c.call("GET", "objects/archive-status/"+plan.Ticket, nil, 404)
	// Missing source is an inspectable preflight issue and requires explicit subset consent.
	body["entries"] = []archivessvc.ArchiveSelection{{Key: "tree/a.txt", Kind: "file"}, {Key: "tree/missing.txt", Kind: "file"}}
	_ = json.Unmarshal(c.call("POST", "objects/archive", body, 200), &plan)
	if plan.Count != 1 || len(plan.Items) != 2 || plan.Items[1].State != "error" {
		t.Fatal(plan)
	}
	c.call("GET", "objects/archive/"+plan.Ticket, nil, 409)
	c.call("PUT", "objects/archive/"+plan.Ticket, map[string]any{"name": "Retry.zip", "base": "", "allowPartial": true}, 200)
	request, _ := http.NewRequest("GET", c.Origin+c.Base+"/objects/archive/"+plan.Ticket, nil)
	response, err := c.Http.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_, params, err := mime.ParseMediaType(response.Header.Get("Content-Disposition"))
	if err != nil || params["filename"] != "Retry.zip" {
		t.Fatal(params, err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	// Unknown folder retries exclude already confirmed sources, even when listing discovers them again.
	body["entries"] = []archivessvc.ArchiveSelection{{Key: "tree/", Kind: "folder"}}
	body["excludeKeys"] = []string{"tree/a.txt"}
	_ = json.Unmarshal(c.call("POST", "objects/archive", body, 200), &plan)
	if plan.Count != 1 || plan.Items[0].Key != "tree/nested/c.txt" {
		t.Fatal(plan)
	}
	body["name"] = "../bad.zip"
	c.call("POST", "objects/archive", body, 400)
}

type archiveDeadlineRecorder struct {
	*httptest.ResponseRecorder
	Deadline, Proceed chan struct{} `json:"-"`
}

func (w *archiveDeadlineRecorder) SetWriteDeadline(time.Time) error {
	close(w.Deadline)
	<-w.Proceed
	return nil
}

func TestArchiveCancelStreamingIntegration(t *testing.T) {
	// An isolated fake S3 stream pauses after headers, so cancellation is deterministic.
	started := make(chan struct{})
	var once sync.Once
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"etag"`)
		w.Header().Set("Last-Modified", time.Unix(1700000000, 0).UTC().Format(http.TimeFormat))
		w.Header().Set("Content-Length", "1048576")
		if r.Method == "HEAD" {
			return
		}
		w.WriteHeader(200)
		w.Write([]byte("data"))
		w.(http.Flusher).Flush()
		once.Do(func() { close(started) })
		<-r.Context().Done()
	}))
	defer fake.Close()
	client, err := minio.New(strings.TrimPrefix(fake.URL, "http://"), &minio.Options{Region: "us-east-1"})
	if err != nil {
		t.Fatal(err)
	}
	app := runtimesvc.New(testConfig())
	s := testsession.New(t, client.EndpointURL().String())
	plan := archiveplan.New(archiveplan.Config{SessionID: s.SessionID(), Bucket: "bucket", Name: "test.zip", Created: time.Now(), Issues: map[string]messages.Ref{}, Items: []archiveplan.ArchiveItem{{Key: "data.bin", Path: "data.bin", Size: 1048576, State: "pending", Info: objectstate.DeletionSnapshot{ETag: "etag", Size: 1048576, Modified: time.Unix(1700000000, 0).UTC()}}}})
	if err := app.StoreSession(s); err != nil {
		t.Fatal(err)
	}
	ticket, err := app.Archives.Put(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	complete := make(chan error, 1)
	writer := &archiveDeadlineRecorder{httptest.NewRecorder(), make(chan struct{}), make(chan struct{})}
	defer func() {
		select {
		case <-writer.Proceed:
		default:
			close(writer.Proceed)
		}
	}()
	go func() {
		defer func() {
			if p := recover(); p != nil {
				if p == http.ErrAbortHandler {
					complete <- nil
				} else {
					panic(p)
				}
			}
		}()
		complete <- archivescontroller.HandleDownloadArchive(app, writer, httptest.NewRequest("GET", "http://ui.example", nil), s, ticket)
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("stream did not start")
	}
	if err := archivescontroller.HandleCancelArchive(app, httptest.NewRecorder(), httptest.NewRequest("DELETE", "http://ui.example", nil), s, ticket); err != nil {
		t.Fatal(err)
	}
	select {
	case <-writer.Deadline:
	case <-time.After(3 * time.Second):
		t.Fatal("write cancellation did not start")
	}
	select {
	case <-complete:
		t.Fatal("handler returned while cancellation still used its ResponseWriter")
	default:
	}
	close(writer.Proceed)
	select {
	case err := <-complete:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stream cancellation did not finish")
	}
	snapshot := plan.Snapshot(ticket)
	if !snapshot.Finished || !snapshot.Cancelled || snapshot.Items[0].State != "cancelled" {
		t.Fatal(plan.Snapshot(ticket))
	}
}
func TestArchiveByteLimit(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(archivessvc.MaxArchiveBytes+1))
		w.Header().Set("ETag", `"etag"`)
		w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
	}))
	defer fake.Close()
	client, _ := minio.New(strings.TrimPrefix(fake.URL, "http://"), &minio.Options{Region: "us-east-1"})
	app := runtimesvc.New(testConfig())
	body := `{"bucket":"bucket","name":"test.zip","keys":["huge.bin"]}`
	request := httptest.NewRequest("POST", "http://ui.example", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	err := archivescontroller.HandlePrepareArchive(app, httptest.NewRecorder(), request, testsession.New(t, client.EndpointURL().String()))
	if apperror.Public(err).Code != "ArchiveTooLarge" {
		t.Fatal(err)
	}
}
