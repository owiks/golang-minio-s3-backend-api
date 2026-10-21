package integration

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7/pkg/credentials"

	apihttp "github.com/owiks/golang-minio-s3-backend-api/src/http/api"
	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/identities"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
)

func TestUserCreationRequiresExplicitAcknowledgementHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, raw, code string
		exists          bool
		status, writes  int
	}{
		{"missing", `{"name":"alice","secretKey":"fixture-secret"}`, "UserCreateAcknowledgementRequired", false, 400, 0},
		{"false", `{"name":"alice","secretKey":"fixture-secret","acknowledgeNonAtomicCreate":false}`, "UserCreateAcknowledgementRequired", false, 400, 0},
		{"not a boolean", `{"name":"alice","secretKey":"fixture-secret","acknowledgeNonAtomicCreate":"true"}`, "", false, 400, 0},
		{"accepted", `{"name":"alice","secretKey":"fixture-secret","acknowledgeNonAtomicCreate":true}`, "", false, 200, 1},
		{"found user despite consent", `{"name":"alice","secretKey":"replacement-secret","acknowledgeNonAtomicCreate":true}`, "AlreadyExists", true, 409, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var writes atomic.Int32
			s := actionCheckSession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					if tc.exists {
						httpcommon.WriteJSON(w, r, 200, map[string]string{"status": "disabled"})
					} else {
						httpcommon.WriteJSON(w, r, 404, map[string]string{"Code": "XMinioAdminNoSuchUser"})
					}
					return
				}
				writes.Add(1)
				w.WriteHeader(200)
			})
			app := runtimesvc.New(testConfig())
			defer app.Close()
			if err := app.StoreSession(s); err != nil {
				t.Fatal(err)
			}
			previous := app.AccessRevision(s)
			r := httptest.NewRequest("POST", "http://ui.example/api/sessions/"+s.SessionID()+"/admin/users", bytes.NewBufferString(tc.raw))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", "http://ui.example")
			r.Header.Set("X-CSRF-Token", s.CSRFToken())
			r.AddCookie(httpcommon.SessionCookie(s, r, false))
			w := httptest.NewRecorder()
			apihttp.NewHandler(app).ServeHTTP(w, r)
			if w.Code != tc.status || writes.Load() != int32(tc.writes) || (app.AccessRevision(s) != previous) != (tc.writes == 1) {
				t.Fatal("unexpected write or revision", w.Code, writes.Load(), w.Body.String())
			}
			if tc.code != "" && !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatal("wrong safe error", w.Body.String())
			}
		})
	}
}

type racingUserCreator struct {
	*storage.AdminClient
	inject func(string)
	writes int
}

func (c *racingUserCreator) GetUserInfo(ctx context.Context, name string) (madmin.UserInfo, error) {
	info, err := c.AdminClient.GetUserInfo(ctx, name)
	if err != nil && apperror.Public(err).Code == "XMinioAdminNoSuchUser" && c.inject != nil {
		c.inject(name)
	}
	return info, err
}
func (c *racingUserCreator) AddUser(ctx context.Context, name, secret string) error {
	c.writes++
	return c.AdminClient.AddUser(ctx, name, secret)
}

func TestUserCreationConsentAndExternalRaceNativeIntegration(t *testing.T) {
	if os.Getenv("MINIO_INTEGRATION") != "1" {
		t.Skip("isolated MinIO fixture required")
	}
	ctx := context.Background()
	endpoint, _ := url.Parse("http://127.0.0.1:19000")
	admin, err := storage.NewAdmin(storage.Config{Endpoint: endpoint, Credentials: credentials.NewStaticV4("minio-web-test", "minio-web-test-secret", "")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"found without consent", "found with consent", "race without consent", "race with consent", "new with consent"} {
		t.Run(scenario, func(t *testing.T) {
			name := "create-contract-" + strings.ToLower(securetoken.New()[:12])
			const original = "External-fixture-original-secret"
			const requested = "Web-fixture-requested-secret"
			t.Cleanup(func() {
				if err := admin.RemoveUser(ctx, name); err != nil {
					t.Error("test user cleanup failed", err)
				}
			})
			makeExternal := func(name string) {
				if err := admin.AddUser(ctx, name, original); err != nil {
					t.Fatal(err)
				}
				if err := admin.SetUserStatus(ctx, name, madmin.AccountDisabled); err != nil {
					t.Fatal(err)
				}
			}
			creator := &racingUserCreator{AdminClient: admin}
			if strings.HasPrefix(scenario, "found") {
				makeExternal(name)
			} else if strings.HasPrefix(scenario, "race") {
				creator.inject = makeExternal
			}
			consent := strings.Contains(scenario, "with consent") && !strings.Contains(scenario, "without")
			err = identities.CreateUser(ctx, creator, identities.CreateUserRequest{Name: name, SecretKey: requested, AcknowledgeNonAtomicCreate: consent})
			code := ""
			if err != nil {
				code = apperror.Public(err).Code
			}
			wantCode, wantWrites := "", 1
			if strings.HasPrefix(scenario, "found") {
				wantCode, wantWrites = "AlreadyExists", 0
			} else if !consent {
				wantCode, wantWrites = "UserCreateAcknowledgementRequired", 0
			}
			if code != wantCode || creator.writes != wantWrites {
				t.Fatal("creation contract changed", scenario, code, creator.writes)
			}
			info, err := admin.GetUserInfo(ctx, name)
			if err != nil {
				t.Fatal(err)
			}
			preserved := wantWrites == 0
			if (info.Status == madmin.AccountDisabled) != preserved {
				t.Fatal("unexpected account status", scenario, info.Status)
			}
			// Enable only in the isolated fixture to prove the original password
			// still authenticates; disabled accounts cannot authenticate at all.
			if preserved {
				if err := admin.SetUserStatus(ctx, name, madmin.AccountEnabled); err != nil {
					t.Fatal(err)
				}
			}
			authenticates := func(secret string) bool {
				client, err := storage.NewAdmin(storage.Config{Endpoint: endpoint, Credentials: credentials.NewStaticV4(name, secret, "")}, nil)
				if err != nil {
					t.Fatal(err)
				}
				_, err = client.AccountInfo(ctx, madmin.AccountOpts{})
				return err == nil
			}
			if authenticates(original) != preserved || authenticates(requested) == preserved {
				t.Fatal("password did not match the stated contract", scenario)
			}
		})
	}
}
