package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/secretbox"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
)

type replacementVerifier struct{}

func (replacementVerifier) Verify(context.Context, authstate.ConnectionView, authstate.Secrets) error {
	return nil
}

func TestStoredCredentialReplacementRefreshesEveryRuntimeCache(t *testing.T) {
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/xml")
		if r.Method == "DELETE" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		fmt.Fprint(w, `<ListAllMyBucketsResult><Buckets></Buckets></ListAllMyBucketsResult>`)
	}))
	defer upstream.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	box, err := secretbox.Ephemeral()
	if err != nil {
		t.Fatal(err)
	}
	manager := authstate.NewManager(authstate.NewMemory(), box, time.Hour)
	cfg := config.Default()
	cfg.Endpoints.Allowed[upstream.URL] = true
	one, two := NewWithAuth(cfg, manager), NewWithAuth(cfg, manager)
	defer one.Close()
	defer two.Close()
	id, cid := securetoken.New(), securetoken.New()
	if err = manager.Bootstrap(ctx, authstate.Bootstrap{ID: id, ConnectionID: cid, Endpoint: upstream.URL, Region: "us-east-1", Mode: "bearer", Digest: authstate.Digest("presented-token"), ExpiresAt: time.Now().Add(time.Hour), Secrets: authstate.Secrets{AccessKey: "fixture-access", SecretKey: "old-secret"}}); err != nil {
		t.Fatal(err)
	}
	access, connection, err := manager.Lookup(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	old := make([]*Session, 0, 2)
	for _, app := range []*State{one, two} {
		s, bindErr := app.BindAccess(ctx, access, connection, false)
		if bindErr != nil {
			t.Fatal(bindErr)
		}
		if _, bindErr = NewClients(s).ListBuckets(ctx); bindErr != nil {
			t.Fatal(bindErr)
		}
		old = append(old, s)
	}
	before := access.Snapshot()
	if err = manager.ReplaceCredentials(ctx, authstate.CredentialGuard(access, false), cid, connection.Snapshot().Revision, authstate.Secrets{AccessKey: "fixture-access", SecretKey: "new-secret", SessionToken: "new-upstream-token"}, replacementVerifier{}); err != nil {
		t.Fatal(err)
	}
	current, changed, err := manager.Lookup(ctx, id)
	if err != nil || !reflect.DeepEqual(current.Snapshot(), before) {
		t.Fatal("replacing upstream credentials changed the API grant", err)
	}
	if changed.Snapshot().Revision != connection.Snapshot().Revision+1 || changed.Snapshot().SecretVersion != connection.Snapshot().SecretVersion+1 {
		t.Fatal("connection version did not advance")
	}
	var freshSessions []*Session
	for i, app := range []*State{one, two} {
		stale := NewClients(old[i])
		if _, err = stale.ListBuckets(ctx); err == nil {
			t.Fatal("retained SDK clients dispatched an old credential")
		}
		if _, err = stale.PresignedGetObject(ctx, "fixture-bucket", "file.txt", time.Minute, nil); err == nil {
			t.Fatal("retained signer issued a URL with an old credential")
		}
		if err = stale.AbortMultipartUpload(ctx, "fixture-bucket", "file.txt", "upload-id"); err == nil {
			t.Fatal("cleanup dispatched an old credential")
		}
		// Deliberately pass the snapshot loaded before replacement. Binding must
		// reload persisted credentials, including in another runtime's hot cache.
		fresh, bindErr := app.BindAccess(ctx, access, connection, false)
		if bindErr != nil {
			t.Fatal(bindErr)
		}
		got, getErr := fresh.credentials.Get()
		if getErr != nil || got.SecretAccessKey != "new-secret" || got.SessionToken != "new-upstream-token" || fresh.sessionCore == old[i].sessionCore {
			t.Fatal("cache retained old native credentials", getErr)
		}
		oldCredentials, getErr := old[i].credentials.Get()
		if getErr != nil || oldCredentials.SecretAccessKey != "old-secret" {
			t.Fatal("replacement mutated an in-flight credential provider", getErr)
		}
		if _, bindErr = NewClients(fresh).ListBuckets(ctx); bindErr != nil {
			t.Fatal(bindErr)
		}
		if _, bindErr = NewClients(fresh).PresignedGetObject(ctx, "fixture-bucket", "file.txt", time.Minute, nil); bindErr != nil {
			t.Fatal(bindErr)
		}
		if app.Session(id).connectionRevision != changed.Snapshot().Revision {
			t.Fatal("cached connection revision is stale")
		}
		freshSessions = append(freshSessions, fresh)
	}
	if requests.Load() != 4 {
		t.Fatal("stale native request reached upstream", requests.Load())
	}
	if err = manager.Revoke(ctx, authstate.CredentialGuard(current, false), id, current.Snapshot().Revision); err != nil {
		t.Fatal(err)
	}
	for _, s := range freshSessions {
		client := NewClients(s)
		if _, err = client.ListBuckets(ctx); err == nil {
			t.Fatal("revocation did not stop native reads")
		}
		if err = client.AbortMultipartUpload(ctx, "fixture-bucket", "file.txt", "upload-id"); err != nil {
			t.Fatal("revocation prevented cleanup with the same native credentials", err)
		}
	}
	if requests.Load() != 6 {
		t.Fatal("revocation dispatched a data read or prevented cleanup", requests.Load())
	}
}
