package runtime

import (
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
)

func TestBearerSessionOwnsOnlyDigestAndExpiresAtBoundary(t *testing.T) {
	endpoint, _ := url.Parse("http://fixture.example")
	tr := http.DefaultTransport.(*http.Transport).Clone()
	defer tr.CloseIdleConnections()
	s, token, err := NewBearerSession(Connection{AccessKey: "fixture", SecretKey: "fixture-secret"}, config.Default(), endpoint, endpoint, tr)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || !s.UsesBearer() || !s.MatchesBearer(token) || s.MatchesBearer("wrong") || s.MatchesBearer("") || s.bearerDigest != sha256.Sum256([]byte(token)) {
		t.Fatal("invalid bearer credential state")
	}
	if s.token != "" || s.csrf != "" || s.MatchesToken(token) || s.MatchesToken("") || s.MatchesCSRF("") {
		t.Fatal("bearer session accepts cookie credentials")
	}
	if s.Expired(s.ExpiresAt().Add(-time.Nanosecond)) || !s.Expired(s.ExpiresAt().Add(time.Nanosecond)) {
		t.Fatal("incorrect expiration boundary")
	}
	for _, value := range []any{s, s.CookieSnapshot()} {
		raw, err := json.Marshal(value)
		if err != nil || strings.Contains(string(raw), token) || string(raw) != "{}" {
			t.Fatal("session state exposed", err)
		}
	}
	cookie, err := NewConnectedSession(Connection{AccessKey: "fixture", SecretKey: "fixture-secret"}, config.Default(), endpoint, endpoint, tr)
	if err != nil {
		t.Fatal(err)
	}
	if cookie.UsesBearer() || cookie.MatchesBearer(cookie.CookieSnapshot().Token) {
		t.Fatal("cookie credential accepted as bearer")
	}
}
