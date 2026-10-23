package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

func TestNonexpiringBearerNativeIntegration(t *testing.T) {
	cookie, s3, bucket := fileFixture(t)
	ttl := int64(0)
	token := issueTestToken(t, cookie.Origin, "minio-web-test", "minio-web-test-secret", &ttl)
	base := "/api/sessions/" + token.ID
	var current struct {
		ExpiresAt time.Time `json:"expiresAt"`
	}
	if err := json.Unmarshal(tokenRequest(t, cookie.Origin, "GET", base+"/credential", token.Token, nil, nil, 200), &current); err != nil || !current.ExpiresAt.IsZero() {
		t.Fatal("permanent Bearer binding acquired a finite expiration", err)
	}
	tokenRequest(t, cookie.Origin, "GET", base+"/admin/buckets", token.Token, nil, nil, 200)

	key := "permanent/бессрочный + файл.txt"
	query := url.Values{"bucket": {bucket}, "key": {key}}.Encode()
	const content = "permanent-bearer-upload"
	tokenRequest(t, cookie.Origin, "PUT", base+"/objects/upload?"+query, token.Token, strings.NewReader(content), map[string]string{"Content-Type": "text/plain", "X-Object-Revision": "absent", "X-Upload-Size": fmtSize(len(content))}, 200)
	if got := tokenRequest(t, cookie.Origin, "GET", base+"/objects/preview?"+query, token.Token, nil, nil, 200); string(got) != content {
		t.Fatal("permanent Bearer upload/read changed the object")
	}
	var deletion struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(tokenRequest(t, cookie.Origin, "GET", base+"/admin/object-delete?"+query, token.Token, nil, nil, 200), &deletion); err != nil || deletion.Revision == "" {
		t.Fatal("permanent Bearer could not prepare conditional deletion", err)
	}
	remove, err := json.Marshal(map[string]string{"revision": deletion.Revision, "confirmation": key})
	if err != nil {
		t.Fatal(err)
	}
	tokenRequest(t, cookie.Origin, "DELETE", base+"/admin/object-delete?"+query, token.Token, bytes.NewReader(remove), map[string]string{"Content-Type": "application/json"}, 200)
	if _, err := s3.StatObject(context.Background(), bucket, key, minio.StatObjectOptions{}); minio.ToErrorResponse(err).Code != "NoSuchKey" {
		t.Fatal("object remained after permanent Bearer deletion", err)
	}

	// Revoke the permanent service credential through the owner's cookie access.
	// Its absence of an expiry must never bypass the persisted revocation fence.
	cookie.call("DELETE", "tokens/"+token.ID, map[string]int64{"revision": 1}, 200)
	tokenRequest(t, cookie.Origin, "GET", base+"/credential", token.Token, nil, nil, 401)
	tokenRequest(t, cookie.Origin, "PUT", base+"/objects/upload?"+query, token.Token, strings.NewReader(content), map[string]string{"Content-Type": "text/plain", "X-Object-Revision": "absent", "X-Upload-Size": fmtSize(len(content))}, 401)
	if _, err := s3.StatObject(context.Background(), bucket, key, minio.StatObjectOptions{}); minio.ToErrorResponse(err).Code != "NoSuchKey" {
		t.Fatal("revoked permanent Bearer reached upstream", err)
	}
	cookie.call("GET", "admin/buckets", nil, 200)
}
