package controllers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/middleware"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	objectstate "github.com/owiks/golang-minio-s3-backend-api/src/services/objects/state"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/transfers"
	"github.com/owiks/golang-minio-s3-backend-api/tests/testsession"
)

func transferBody() transfers.FileTransferRequest {
	return transfers.FileTransferRequest{
		SourceBucket: "source-bucket", SourceKey: "folder/file.txt",
		SourceRevision:    objectstate.DeletionRevision(objectstate.DeletionSnapshot{ETag: "source-etag", Size: 6, Modified: time.Date(2026, time.October, 20, 10, 0, 0, 0, time.UTC)}),
		DestinationBucket: "target-bucket", DestinationKey: "copies/file.txt", DestinationRevision: "absent",
	}
}

func TestFileTransferHTTPReplaysWithCurrentLanguageAndStableCommandID(t *testing.T) {
	var requests, parts atomic.Int32
	var copied atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/xml")
		switch {
		case r.Method == "HEAD" && r.URL.Path == "/source-bucket/folder/file.txt":
			w.Header().Set("Content-Length", "6")
			w.Header().Set("ETag", `"source-etag"`)
			w.Header().Set("Last-Modified", "Tue, 20 Oct 2026 10:00:00 GMT")
			w.Header().Set("Content-Type", "text/plain")
		case r.Method == "HEAD" && r.URL.Path == "/target-bucket/copies/file.txt":
			if !copied.Load() {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Length", "6")
			w.Header().Set("ETag", `"destination-etag"`)
			w.Header().Set("Last-Modified", "Tue, 20 Oct 2026 10:00:00 GMT")
		case r.Method == "GET" && r.URL.Query().Has("tagging"):
			fmt.Fprint(w, `<Tagging><TagSet></TagSet></Tagging>`)
		case r.Method == "POST" && r.URL.Query().Has("uploads"):
			fmt.Fprint(w, `<InitiateMultipartUploadResult><Bucket>target-bucket</Bucket><Key>copies/file.txt</Key><UploadId>copy-upload</UploadId></InitiateMultipartUploadResult>`)
		case r.Method == "PUT" && r.URL.Query().Get("uploadId") == "copy-upload":
			parts.Add(1)
			fmt.Fprint(w, `<CopyPartResult><ETag>"part-etag"</ETag><LastModified>2026-10-20T10:00:00Z</LastModified></CopyPartResult>`)
		case r.Method == "POST" && r.URL.Query().Get("uploadId") == "copy-upload":
			copied.Store(true)
			fmt.Fprint(w, `<CompleteMultipartUploadResult><Location>/target-bucket/copies/file.txt</Location><Bucket>target-bucket</Bucket><Key>copies/file.txt</Key><ETag>"destination-etag"</ETag></CompleteMultipartUploadResult>`)
		default:
			t.Error("unexpected native request", r.Method, r.URL.Path, r.URL.RawQuery)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer upstream.Close()
	app := runtime.New(config.Config{})
	defer app.Close()
	session := testsession.New(t, upstream.URL)
	input, _ := json.Marshal(transferBody())
	id := ""
	firstNativeRequests := int32(0)
	previousMessage := ""
	for _, lang := range []string{"en", "ru"} {
		r := httptest.NewRequest("POST", "/", bytes.NewReader(input))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept-Language", lang)
		r.Header.Set("Idempotency-Key", "single-native-copy")
		r = middleware.LocalizeRequest(r, "auto", nil)
		w := httptest.NewRecorder()
		if err := HandleFileTransfer(app, w, r, session); err != nil {
			t.Fatal("handler", apperror.Public(err).Code, err)
		}
		var response struct {
			Copied, Moved bool
			Message       string
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || w.Code != 200 || !response.Copied || response.Moved || response.Message == "" || w.Header().Get("Content-Language") != lang {
			t.Fatal("HTTP result", lang, w.Code, w.Body.String(), err)
		}
		if lang == "en" {
			id, firstNativeRequests, previousMessage = w.Header().Get("X-Command-ID"), requests.Load(), response.Message
			if id == "" || w.Header().Get("Idempotency-Replayed") != "" || parts.Load() != 1 {
				t.Fatal("initial command envelope", w.Header(), parts.Load())
			}
		} else if w.Header().Get("X-Command-ID") != id || w.Header().Get("Idempotency-Replayed") != "true" || requests.Load() != firstNativeRequests || response.Message == previousMessage {
			t.Fatal("replay lost identity, language or repeated upstream", w.Header(), response.Message, requests.Load())
		}
	}
}

func TestFileTransferRejectsEmptyAndRepeatedIdempotencyHeaders(t *testing.T) {
	app := runtime.New(config.Config{})
	defer app.Close()
	input, _ := json.Marshal(transferBody())
	for _, values := range [][]string{{""}, {"one", "two"}, {"one", "one"}, {"", "one"}} {
		t.Run(strings.Join(values, "/"), func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", bytes.NewReader(input))
			r.Header.Set("Content-Type", "application/json")
			r.Header["Idempotency-Key"] = values
			w := httptest.NewRecorder()
			// A nil session proves validation precedes client creation and I/O.
			err := HandleFileTransfer(app, w, r, nil)
			if apperror.Public(err).Code != "InvalidIdempotencyKey" || apperror.Public(err).Status != 400 || w.Body.Len() != 0 || w.Header().Get("X-Command-ID") != "" {
				t.Fatal("malformed header accepted", values, err, w.Body.String())
			}
		})
	}
}
