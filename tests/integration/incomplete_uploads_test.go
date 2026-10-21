package integration

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	uploadscontroller "minio-web/api/src/http/api/routes/uploads/controllers"
	"minio-web/api/src/services/apperror"
	runtimesvc "minio-web/api/src/services/runtime"
	uploadssvc "minio-web/api/src/services/uploads"
	"minio-web/api/tests/testsession"
)

func TestIncompleteAbortVerification(t *testing.T) {
	for _, outcome := range []string{"cancelled", "remaining", "uncertain", "absent", "changed"} {
		t.Run(outcome, func(t *testing.T) {
			reads, deletes := 0, 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("uploadId") != "exact-id" {
					t.Errorf("wrong upload ID: %s", r.URL.RawQuery)
				}
				if r.Method == "DELETE" {
					deletes++
					w.WriteHeader(204)
					return
				}
				reads++
				w.Header().Set("Content-Type", "application/xml")
				if outcome == "absent" || reads > 1 && outcome == "cancelled" {
					w.WriteHeader(404)
					fmt.Fprint(w, `<Error><Code>NoSuchUpload</Code></Error>`)
					return
				}
				if reads > 1 && outcome == "uncertain" {
					w.WriteHeader(403)
					fmt.Fprint(w, `<Error><Code>AccessDenied</Code></Error>`)
					return
				}
				fmt.Fprint(w, `<ListPartsResult><Bucket>fixture</Bucket><Key>key</Key><UploadId>exact-id</UploadId><IsTruncated>false</IsTruncated><Part><PartNumber>1</PartNumber><LastModified>2026-10-01T00:00:00Z</LastModified><ETag>one</ETag><Size>7</Size></Part></ListPartsResult>`)
			}))
			defer upstream.Close()
			client, err := minio.New(strings.TrimPrefix(upstream.URL, "http://"), &minio.Options{Creds: credentials.NewStaticV4("test", "test-secret", ""), Region: "us-east-1", MaxRetries: 1})
			if err != nil {
				t.Fatal(err)
			}
			s := testsession.New(t, client.EndpointURL().String())
			req := httptest.NewRequest("GET", "/", nil)
			parts, err := uploadssvc.InspectParts(req.Context(), runtimesvc.NewClients(s), "fixture", "key", "exact-id")
			if err != nil && outcome != "absent" {
				t.Fatal(err)
			}
			reads = 0
			rev := parts.Revision
			if outcome == "changed" {
				rev = "old"
			}
			raw, _ := json.Marshal(map[string]string{"bucket": "fixture", "key": "key", "uploadId": "exact-id", "revision": rev, "confirmation": "ОТМЕНИТЬ"})
			response := httptest.NewRecorder()
			request := httptest.NewRequest("POST", "/", strings.NewReader(string(raw)))
			request.Header.Set("Content-Type", "application/json")
			err = uploadscontroller.HandleAbortIncompleteUpload((&runtimesvc.State{}), response, request, s)
			if outcome == "changed" {
				var issue *apperror.Error
				if !errors.As(err, &issue) || issue.Code != "UploadChanged" || deletes != 0 {
					t.Fatal("changed upload was cancelled", err, deletes)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var result struct {
				State string `json:"state"`
			}
			_ = json.Unmarshal(response.Body.Bytes(), &result)
			if result.State != outcome {
				t.Fatalf("state=%s want=%s body=%s", result.State, outcome, response.Body.String())
			}
			if outcome == "absent" && deletes != 0 || outcome != "absent" && deletes != 1 {
				t.Fatal("unexpected abort count", deletes)
			}
		})
	}
}

func TestIncompleteSummaryBoundedPrefixScan(t *testing.T) {
	reads := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads++
		q := r.URL.Query()
		if q.Get("prefix") != "" {
			t.Error("must filter locally")
		}
		if reads > 1 && q.Get("key-marker") != strconv.Itoa(reads-1) {
			t.Error("cursor lost", q)
		}
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprintf(w, `<ListMultipartUploadsResult><Bucket>fixture</Bucket><IsTruncated>true</IsTruncated><NextKeyMarker>%d</NextKeyMarker><NextUploadIdMarker>id</NextUploadIdMarker><Upload><Key>other/file</Key><UploadId>id</UploadId></Upload></ListMultipartUploadsResult>`, reads)
	}))
	defer upstream.Close()
	client, err := minio.New(strings.TrimPrefix(upstream.URL, "http://"), &minio.Options{Region: "us-east-1"})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	err = uploadscontroller.HandleListIncompleteUploads((&runtimesvc.State{}), response, httptest.NewRequest("GET", "/?bucket=fixture&prefix=backups%2F&summary=1", nil), testsession.New(t, client.EndpointURL().String()))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		HasMore bool   `json:"hasMore"`
		Marker  string `json:"keyMarker"`
		Items   []any  `json:"items"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &result)
	if reads != 8 || !result.HasMore || result.Marker != "8" || len(result.Items) != 0 {
		t.Fatal("unbounded scan or wrong cursor", reads, result)
	}
}
