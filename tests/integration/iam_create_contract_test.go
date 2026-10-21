package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7/pkg/credentials"
	apihttp "minio-web/api/src/http/api"
	"minio-web/api/src/http/api/common"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/identities"
	runtimesvc "minio-web/api/src/services/runtime"
	"minio-web/api/src/services/securetoken"
	"minio-web/api/src/services/storage"
)

func TestPolicyAndGroupCreateConsentHTTP(t *testing.T) {
	for _, kind := range []string{"policies", "groups"} {
		for _, tc := range []struct {
			name, consent  string
			exists         bool
			status, writes int
		}{
			{"missing", "", false, 400, 0}, {"false", `,"acknowledgeNonAtomicCreate":false`, false, 400, 0},
			{"wrong type", `,"acknowledgeNonAtomicCreate":"true"`, false, 400, 0},
			{"accepted", `,"acknowledgeNonAtomicCreate":true`, false, 200, 1},
			{"existing", `,"acknowledgeNonAtomicCreate":true`, true, 409, 0},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				var writes atomic.Int32
				s := actionCheckSession(t, func(w http.ResponseWriter, r *http.Request) {
					if r.Method == "GET" {
						if tc.exists {
							if kind == "policies" {
								_, _ = w.Write([]byte(`{"Version":"2012-10-17","Statement":[]}`))
							} else {
								common.WriteJSON(w, 200, map[string]any{"name": "fixture", "status": "disabled", "members": []string{"external"}})
							}
						} else {
							code := "XMinioAdminNoSuchPolicy"
							if kind == "groups" {
								code = "XMinioAdminNoSuchGroup"
							}
							common.WriteJSON(w, 404, map[string]string{"Code": code})
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
				before := app.AccessRevision(s)
				fields := `"document":{"Version":"2012-10-17","Statement":[]}`
				if kind == "groups" {
					fields = `"members":["alice"]`
				}
				r := httptest.NewRequest("POST", "http://ui.example/api/sessions/"+s.SessionID()+"/admin/"+kind, strings.NewReader(`{"name":"fixture",`+fields+tc.consent+`}`))
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Origin", "http://ui.example")
				r.Header.Set("X-CSRF-Token", s.CSRFToken())
				r.AddCookie(common.SessionCookie(s, r, false))
				w := httptest.NewRecorder()
				apihttp.NewHandler(app).ServeHTTP(w, r)
				if w.Code != tc.status || writes.Load() != int32(tc.writes) || (app.AccessRevision(s) != before) != (tc.writes == 1) {
					t.Fatalf("status=%d writes=%d body=%s", w.Code, writes.Load(), w.Body.String())
				}
			})
		}
	}
}

type racingPolicyCreator struct {
	*storage.AdminClient
	inject func(string)
	writes int
}

func (c *racingPolicyCreator) InfoCannedPolicy(ctx context.Context, name string) ([]byte, error) {
	raw, err := c.AdminClient.InfoCannedPolicy(ctx, name)
	if identities.Missing(err, "XMinioAdminNoSuchPolicy") && c.inject != nil {
		c.inject(name)
	}
	return raw, err
}
func (c *racingPolicyCreator) AddCannedPolicy(ctx context.Context, name string, doc []byte) error {
	c.writes++
	return c.AdminClient.AddCannedPolicy(ctx, name, doc)
}

type racingGroupCreator struct {
	*storage.AdminClient
	inject func(string)
	writes int
}

func (c *racingGroupCreator) GetGroupDescription(ctx context.Context, name string) (*madmin.GroupDesc, error) {
	group, err := c.AdminClient.GetGroupDescription(ctx, name)
	if identities.Missing(err, "XMinioAdminNoSuchGroup") && c.inject != nil {
		c.inject(name)
	}
	return group, err
}
func (c *racingGroupCreator) UpdateGroupMembers(ctx context.Context, change madmin.GroupAddRemove) error {
	c.writes++
	return c.AdminClient.UpdateGroupMembers(ctx, change)
}

func TestPolicyAndGroupCreationExternalRaceConsentNativeIntegration(t *testing.T) {
	if os.Getenv("MINIO_INTEGRATION") != "1" {
		t.Skip("isolated MinIO fixture required")
	}
	endpoint, _ := url.Parse("http://127.0.0.1:19000")
	tr := http.DefaultTransport.(*http.Transport).Clone()
	defer tr.CloseIdleConnections()
	admin, err := storage.NewAdmin(storage.Config{Endpoint: endpoint, Credentials: credentials.NewStaticV4("minio-web-test", "minio-web-test-secret", "")}, tr)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	prefix := "iam-race-" + securetoken.New()[:12]
	external, member := prefix+"-ext", prefix+"-web"
	for _, name := range []string{external, member} {
		if err := admin.AddUser(ctx, name, "fixture-race-secret"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = admin.RemoveUser(context.Background(), name) })
	}
	original := json.RawMessage(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::fixture/*"]}]}`)
	replacement := json.RawMessage(bytes.ReplaceAll(original, []byte("GetObject"), []byte("PutObject")))
	for _, consent := range []bool{false, true} {
		t.Run(map[bool]string{false: "no consent", true: "accepted risk"}[consent], func(t *testing.T) {
			suffix := "-no"
			if consent {
				suffix = "-yes"
			}
			policy, group := prefix+suffix+"-policy", prefix+suffix+"-group"
			t.Cleanup(func() {
				_, _ = admin.DetachPolicy(context.Background(), madmin.PolicyAssociationReq{User: external, Policies: []string{policy}})
				_ = admin.RemoveCannedPolicy(context.Background(), policy)
				_ = admin.UpdateGroupMembers(context.Background(), madmin.GroupAddRemove{Group: group, Members: []string{external, member}, IsRemove: true})
				_ = admin.UpdateGroupMembers(context.Background(), madmin.GroupAddRemove{Group: group, IsRemove: true})
			})
			pc := &racingPolicyCreator{AdminClient: admin, inject: func(name string) {
				if err := admin.AddCannedPolicy(ctx, name, original); err != nil {
					t.Fatal(err)
				}
				if _, err := admin.AttachPolicy(ctx, madmin.PolicyAssociationReq{User: external, Policies: []string{name}}); err != nil {
					t.Fatal(err)
				}
			}}
			err := identities.CreatePolicy(ctx, pc, identities.CreatePolicyCommand{Name: policy, Document: replacement, AcknowledgeNonAtomicCreate: consent})
			want := "PolicyCreateAcknowledgementRequired"
			if consent {
				want = ""
			}
			assertCreateResult(t, err, pc.writes, want, consent)
			raw, err := admin.InfoCannedPolicy(ctx, policy)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "s3:PutObject") != consent {
				t.Fatal("policy changed without consent")
			}
			info, err := admin.GetUserInfo(ctx, external)
			if err != nil || !strings.Contains(info.PolicyName, policy) {
				t.Fatal("policy attachment lost", err)
			}
			pc.inject = nil
			err = identities.CreatePolicy(ctx, pc, identities.CreatePolicyCommand{Name: policy, Document: replacement, AcknowledgeNonAtomicCreate: true})
			if err == nil || apperror.Public(err).Code != "AlreadyExists" {
				t.Fatal("observed policy modified", err)
			}
			gc := &racingGroupCreator{AdminClient: admin, inject: func(name string) {
				if err := admin.UpdateGroupMembers(ctx, madmin.GroupAddRemove{Group: name, Members: []string{external}}); err != nil {
					t.Fatal(err)
				}
				if err := admin.SetGroupStatus(ctx, name, madmin.GroupDisabled); err != nil {
					t.Fatal(err)
				}
			}}
			err = identities.CreateGroup(ctx, gc, identities.CreateGroupRequest{Name: group, Members: []string{member}, AcknowledgeNonAtomicCreate: consent})
			want = "GroupCreateAcknowledgementRequired"
			if consent {
				want = ""
			}
			assertCreateResult(t, err, gc.writes, want, consent)
			desc, err := admin.GetGroupDescription(ctx, group)
			if err != nil {
				t.Fatal(err)
			}
			count := 1
			if consent {
				count = 2
			}
			if len(desc.Members) != count || desc.Status != "disabled" {
				t.Fatal("unexpected group state", desc)
			}
			gc.inject = nil
			err = identities.CreateGroup(ctx, gc, identities.CreateGroupRequest{Name: group, Members: []string{member}, AcknowledgeNonAtomicCreate: true})
			if err == nil || apperror.Public(err).Code != "AlreadyExists" {
				t.Fatal("observed group modified", err)
			}
		})
	}
}
func assertCreateResult(t *testing.T, err error, writes int, code string, consent bool) {
	t.Helper()
	want := 0
	if consent {
		want = 1
	}
	if writes != want || (code == "" && err != nil) || (code != "" && (err == nil || apperror.Public(err).Code != code)) {
		t.Fatalf("unexpected create result: err=%v writes=%d", err, writes)
	}
}
