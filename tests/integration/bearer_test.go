package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/sessions"
)

// All requests use a separate client with no cookie jar, Origin or CSRF.
func tokenRequest(t *testing.T, origin, method, path, token string, input io.Reader, headers map[string]string, want int) []byte {
	t.Helper()
	r, err := http.NewRequest(method, origin+path, input)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range headers {
		r.Header.Set(key, value)
	}
	client := &http.Client{Timeout: 40 * time.Second}
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != want {
		t.Fatalf("%s %s: got %d want %d: %s", method, path, response.StatusCode, want, data)
	}
	if len(response.Cookies()) != 0 || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("bearer response set cookie or allowed caching")
	}
	assertResponseContract(t, method, r.URL.Path, response.StatusCode, response.Header.Get("Content-Type"), data)
	return data
}

func issueTestToken(t *testing.T, origin, user, secret string, ttl *int64) sessions.TokenResponse {
	t.Helper()
	request := sessions.TokenRequest{Connection: runtime.Connection{Endpoint: "http://127.0.0.1:19000", AccessKey: user, SecretKey: secret}, TTLSeconds: ttl}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	data := tokenRequest(t, origin, "POST", "/api/tokens", "", bytes.NewReader(raw), map[string]string{"Content-Type": "application/json"}, 201)
	var result sessions.TokenResponse
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	want := int64(3600)
	if ttl != nil {
		want = *ttl
	}
	expiryValid := result.ExpiresAt.IsZero()
	if want != 0 {
		expiryValid = !result.ExpiresAt.IsZero() && !result.ExpiresAt.Before(before.Add(time.Duration(want)*time.Second)) && !result.ExpiresAt.After(time.Now().Add(time.Duration(want)*time.Second))
	}
	if result.Token == "" || result.TokenType != "Bearer" || result.ID == "" || result.ExpiresIn != want || !expiryValid {
		t.Fatal("invalid token response")
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["csrf"] != nil || fields["secretKey"] != nil || fields["sessionToken"] != nil {
		t.Fatal("cookie or S3 credentials exposed")
	}
	return result
}

func TestBearerNativeIntegration(t *testing.T) {
	c, _, bucket := fileFixture(t)
	token := issueTestToken(t, c.Origin, "minio-web-test", "minio-web-test-secret", nil)
	base := "/api/sessions/" + token.ID
	tokenRequest(t, c.Origin, "GET", base+"/admin/buckets", token.Token, nil, nil, 200)
	query := url.Values{"bucket": {bucket}, "key": {"bearer + файл.txt"}}.Encode()
	tokenRequest(t, c.Origin, "PUT", base+"/objects/upload?"+query, token.Token, strings.NewReader("bearer-upload"), map[string]string{"Content-Type": "text/plain", "X-Object-Revision": "absent", "X-Upload-Size": "13"}, 200)
	data := tokenRequest(t, c.Origin, "GET", base+"/objects/preview?"+query, token.Token, nil, nil, 200)
	if string(data) != "bearer-upload" {
		t.Fatal("file content changed", string(data))
	}
	tokenRequest(t, c.Origin, "GET", base+"/admin/buckets", "wrong", nil, nil, 401)
	// A token cannot impersonate an independent browser session.
	tokenRequest(t, c.Origin, "GET", c.Base+"/admin/buckets", token.Token, nil, nil, 401)
	tokenRequest(t, c.Origin, "DELETE", base, token.Token, nil, nil, 200)
	tokenRequest(t, c.Origin, "GET", base+"/admin/buckets", token.Token, nil, nil, 401)
	tokenRequest(t, c.Origin, "DELETE", base, token.Token, nil, nil, 401)
	c.call("GET", "admin/buckets", nil, 200)

	ttl := int64(1)
	short := issueTestToken(t, c.Origin, "minio-web-test", "minio-web-test-secret", &ttl)
	time.Sleep(time.Until(short.ExpiresAt) + 10*time.Millisecond)
	tokenRequest(t, c.Origin, "GET", "/api/sessions/"+short.ID+"/admin/buckets", short.Token, nil, nil, 401)
}

func TestBearerPreservesMinIOPermissionsNativeIntegration(t *testing.T) {
	c, _, bucket := fileFixture(t)
	user := "bearer-" + strings.ToLower(strings.NewReplacer("_", "a", "-", "b").Replace(securetoken.New()[:10]))
	const secret = "Bearer-readonly-fixture-secret-123"
	c.call("POST", "admin/users", map[string]any{"name": user, "secretKey": secret, "acknowledgeNonAtomicCreate": true}, 200)
	defer c.call("DELETE", "admin/users/"+user, map[string]string{"confirmation": user}, 200)
	c.call("POST", "admin/bindings", map[string]any{"user": user, "policy": "readonly"}, 200)
	defer c.call("POST", "admin/bindings", map[string]any{"user": user, "policy": "readonly", "remove": true}, 200)
	token := issueTestToken(t, c.Origin, user, secret, nil)
	base := "/api/sessions/" + token.ID
	tokenRequest(t, c.Origin, "GET", base+"/admin/buckets", token.Token, nil, nil, 200)
	tokenRequest(t, c.Origin, "GET", base+"/admin/users", token.Token, nil, nil, 403)
	query := url.Values{"bucket": {bucket}, "key": {"readonly.txt"}}.Encode()
	tokenRequest(t, c.Origin, "PUT", base+"/objects/upload?"+query, token.Token, strings.NewReader("denied"), map[string]string{"Content-Type": "text/plain", "X-Object-Revision": "absent", "X-Upload-Size": "6"}, 403)
	tokenRequest(t, c.Origin, "DELETE", base, token.Token, nil, nil, 200)
}
