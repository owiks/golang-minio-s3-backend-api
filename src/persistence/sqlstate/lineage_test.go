package sqlstate

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/secretbox"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/tests/databasefixtures"
	"github.com/owiks/golang-minio-s3-backend-api/tests/statefixtures"
)

func TestSQLGetValidatesDelegationSnapshot(t *testing.T) {
	drivers := []string{"sqlite"}
	if os.Getenv("STATE_INTEGRATION") == "1" {
		drivers = append(drivers, "postgres", "mysql")
	}
	for _, driver := range drivers {
		t.Run(driver, func(t *testing.T) {
			cfg := databasefixtures.Database(t, driver)
			statefixtures.Migrate(t, cfg)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			s, err := Open(ctx, Config{Database: cfg.Database})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			box, err := secretbox.Ephemeral()
			if err != nil {
				t.Fatal(err)
			}
			m := authstate.NewManager(s, box, time.Hour)
			if err = m.Bootstrap(ctx, authstate.Bootstrap{ID: "root", ConnectionID: "connection", Endpoint: "https://s3.example.test", Mode: "bearer", Digest: authstate.Digest("secret"), ExpiresAt: time.Now().Add(time.Hour), Secrets: authstate.Secrets{AccessKey: "fixture", SecretKey: "private"}}); err != nil {
				t.Fatal(err)
			}
			root, _, err := m.Lookup(ctx, "root")
			if err != nil {
				t.Fatal(err)
			}
			issued, err := m.Issue(ctx, authstate.CredentialGuard(root, false), "connection", authstate.IssueRequest{TTLSeconds: 600, Policy: authstate.Policy{Scopes: []string{"objects:read"}}})
			if err != nil {
				t.Fatal(err)
			}
			child, _, err := s.Get(ctx, issued.ID)
			if err != nil || child.Snapshot().ParentID != "root" {
				t.Fatal("delegated grant cannot be read", err)
			}
			write := func(d authstate.AccessData) {
				t.Helper()
				raw, err := json.Marshal(d)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = s.db.ExecContext(ctx, s.q("UPDATE api_accesses SET payload=? WHERE id=?"), string(raw), d.ID); err != nil {
					t.Fatal(err)
				}
			}
			r := root.Snapshot()
			r.Active = false
			write(r)
			if _, _, err = s.Get(ctx, issued.ID); err == nil {
				t.Fatal("revoked parent accepted")
			}
			d := child.Snapshot()
			d.Active = false
			write(d)
			if got, _, err := s.Get(ctx, issued.ID); err != nil || got.Snapshot().Active {
				t.Fatal("inactive child no longer inspectable", err)
			}
			d.ParentID = "missing"
			write(d)
			if _, _, err = s.Get(ctx, issued.ID); err == nil {
				t.Fatal("orphan accepted")
			}
			if _, _, err = s.Get(ctx, "root"); err != nil {
				t.Fatal("failed lineage read leaked the SQL transaction", err)
			}
		})
	}
}
