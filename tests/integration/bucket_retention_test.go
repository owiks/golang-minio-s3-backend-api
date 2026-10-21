package integration

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	apihttp "github.com/owiks/golang-minio-s3-backend-api/src/http/api"
	bucketssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/buckets"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
)

func TestRetentionValidation(t *testing.T) {
	for _, raw := range []string{"", `{}`, `{"mode":"GOVERNANCE","duration":0,"unit":"DAYS"}`, `{"mode":"UNKNOWN","duration":30,"unit":"DAYS"}`, `{"mode":"COMPLIANCE","duration":101,"unit":"YEARS"}`, `{"mode":"GOVERNANCE","duration":1.5,"unit":"DAYS"}`, `{"mode":"GOVERNANCE","duration":1,"unit":"DAYS","unexpected":true}`} {
		if _, err := bucketssvc.ParseRetention(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	for _, raw := range []string{`null`, `{"mode":"GOVERNANCE","duration":30,"unit":"DAYS"}`, `{"mode":"COMPLIANCE","duration":1,"unit":"YEARS"}`} {
		if _, err := bucketssvc.ParseRetention(json.RawMessage(raw)); err != nil {
			t.Fatal(raw, err)
		}
	}
}

type retentionField[T any] struct {
	Value    T      `json:"value"`
	Revision string `json:"revision"`
	Error    string `json:"error"`
}
type retentionSnapshot struct {
	Versioning retentionField[bucketssvc.VersioningSetting] `json:"versioning"`
	Locking    retentionField[bucketssvc.LockingSetting]    `json:"locking"`
}

func TestBucketRetentionIntegration(t *testing.T) {
	if os.Getenv("MINIO_INTEGRATION") != "1" {
		t.Skip("requires docker-compose.test.yml fixture")
	}
	cfg := testConfig()
	cfg.Endpoints.Allowed["http://127.0.0.1:19000"] = true
	app := runtimesvc.New(cfg)
	defer app.Close()
	srv := httptest.NewServer(apihttp.NewHandler(app))
	defer srv.Close()
	root := loginTest(t, srv.URL, "minio-web-test", "minio-web-test-secret")
	suffix := strings.NewReplacer("_", "a", "-", "b").Replace(strings.ToLower(securetoken.New()[:10]))
	normal, locked := "retention-"+suffix, "locked-"+suffix
	fixture, err := minio.New("127.0.0.1:19000", &minio.Options{Creds: credentials.NewStaticV4("minio-web-test", "minio-web-test-secret", ""), Region: "us-east-1"})
	if err != nil {
		t.Fatal(err)
	}
	var versionIDs []string
	defer func() {
		for _, id := range versionIDs {
			_ = fixture.RemoveObject(context.Background(), locked, "retained.txt", minio.RemoveObjectOptions{VersionID: id, GovernanceBypass: true})
		}
		_ = fixture.RemoveBucket(context.Background(), normal)
		_ = fixture.RemoveBucket(context.Background(), locked)
	}()
	decode := func(raw []byte) retentionSnapshot {
		t.Helper()
		var s retentionSnapshot
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatal(err)
		}
		if s.Versioning.Error != "" || s.Locking.Error != "" || s.Versioning.Revision == "" || s.Locking.Revision == "" {
			t.Fatalf("unreadable retention: %s", raw)
		}
		return s
	}
	get := func(bucket string) retentionSnapshot {
		t.Helper()
		return decode(root.call("GET", "admin/buckets/"+bucket+"/details", nil, 200))
	}
	putVersion := func(bucket, status, revision string, want int) []byte {
		t.Helper()
		return root.call("PUT", "admin/buckets/"+bucket+"/versioning", map[string]any{"status": status, "revision": revision}, want)
	}
	putLock := func(bucket string, enabled bool, rule any, revision string, want int) []byte {
		t.Helper()
		return root.call("PUT", "admin/buckets/"+bucket+"/locking", map[string]any{"enabled": enabled, "defaultRetention": rule, "revision": revision}, want)
	}
	root.call("POST", "admin/buckets", map[string]any{"name": normal}, 200)
	s := get(normal)
	initial := s.Versioning.Revision
	putVersion(normal, "Disabled", initial, 400)
	putLock(normal, true, nil, s.Locking.Revision, 409)
	s = decode(putVersion(normal, "Enabled", initial, 200))
	if s.Versioning.Value.Status != "Enabled" {
		t.Fatal(s)
	}
	putVersion(normal, "Suspended", initial, 409)
	s = decode(putVersion(normal, "Suspended", s.Versioning.Revision, 200))
	if s.Versioning.Value.Status != "Suspended" {
		t.Fatal(s)
	}
	s = decode(putVersion(normal, "Enabled", s.Versioning.Revision, 200))
	// The pinned community MinIO fixture requires locking at bucket creation.
	raw := putLock(normal, true, nil, s.Locking.Revision, 409)
	if !strings.Contains(string(raw), "ObjectLockUnavailable") {
		t.Fatal(string(raw))
	}
	if get(normal).Locking.Value.Enabled {
		t.Fatal("failed enable changed locking")
	}
	err = fixture.SetBucketVersioning(context.Background(), normal, minio.BucketVersioningConfiguration{Status: "Enabled", ExcludedPrefixes: []minio.ExcludedPrefix{{Prefix: "skip/"}}, ExcludeFolders: true})
	if err != nil {
		t.Fatal(err)
	}
	s = get(normal)
	decode(putVersion(normal, "Enabled", s.Versioning.Revision, 200))
	actual, err := fixture.GetBucketVersioning(context.Background(), normal)
	if err != nil || !actual.ExcludeFolders || len(actual.ExcludedPrefixes) != 1 || actual.ExcludedPrefixes[0].Prefix != "skip/" {
		t.Fatal("versioning extensions lost", actual, err)
	}
	root.call("POST", "admin/buckets", map[string]any{"name": locked, "objectLock": true}, 200)
	s = get(locked)
	if !s.Locking.Value.Enabled || s.Versioning.Value.Status != "Enabled" {
		t.Fatal("creation did not enable both controls", s)
	}
	initial = s.Locking.Revision
	rule := map[string]any{"mode": "GOVERNANCE", "duration": 7, "unit": "DAYS"}
	s = decode(putLock(locked, true, rule, initial, 200))
	if s.Locking.Value.Mode == nil || *s.Locking.Value.Mode != minio.Governance || s.Locking.Value.Validity == nil || *s.Locking.Value.Validity != 7 || s.Locking.Value.Unit == nil || *s.Locking.Value.Unit != minio.Days {
		t.Fatal(s)
	}
	putLock(locked, true, nil, initial, 409)
	putLock(locked, false, nil, s.Locking.Revision, 400)
	root.call("PUT", "admin/buckets/"+locked+"/locking", map[string]any{"enabled": true, "revision": s.Locking.Revision}, 400)
	putVersion(locked, "Suspended", s.Versioning.Revision, 409)
	// A default rule affects uploaded versions; clearing it must not release those versions.
	object, err := fixture.PutObject(context.Background(), locked, "retained.txt", strings.NewReader("test"), 4, minio.PutObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	versionIDs = append(versionIDs, object.VersionID)
	mode, until, err := fixture.GetObjectRetention(context.Background(), locked, "retained.txt", object.VersionID)
	if err != nil || mode == nil || *mode != minio.Governance || until == nil || until.Before(time.Now().Add(6*24*time.Hour)) {
		t.Fatal(mode, until, err)
	}
	s = decode(putLock(locked, true, nil, s.Locking.Revision, 200))
	if !s.Locking.Value.Enabled || s.Locking.Value.Mode != nil {
		t.Fatal("clearing default disabled locking", s)
	}
	_, after, err := fixture.GetObjectRetention(context.Background(), locked, "retained.txt", object.VersionID)
	if err != nil || after == nil || !after.Equal(*until) {
		t.Fatal("clearing default released object", after, err)
	}
	// No objects are uploaded under compliance so fixture cleanup never requires bypassing it.
	s = decode(putLock(locked, true, map[string]any{"mode": "COMPLIANCE", "duration": 1, "unit": "YEARS"}, s.Locking.Revision, 200))
	if s.Locking.Value.Mode == nil || *s.Locking.Value.Mode != minio.Compliance || s.Locking.Value.Unit == nil || *s.Locking.Value.Unit != minio.Years {
		t.Fatal(s)
	}
	decode(putLock(locked, true, nil, s.Locking.Revision, 200))
}
