package migrations

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/secretbox"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
)

func fixtureBox(t *testing.T) *secretbox.Box {
	t.Helper()
	box, err := secretbox.New(secretbox.Keyring{Active: "fixture-key", Keys: map[string]string{"fixture-key": base64.StdEncoding.EncodeToString(make([]byte, 32))}})
	if err != nil {
		t.Fatal(err)
	}
	return box
}

func seedAuthentication(t *testing.T, r *Runner) {
	t.Helper()
	now := time.Now().UTC()
	owner := authstate.Owner("https://storage.example.test", "fixture")
	c := authstate.ConnectionData{ID: "legacy-connection", Owner: owner, Endpoint: "https://storage.example.test", Region: "us-east-1", Actor: authstate.Digest("fixture"), Revision: 11, SecretVersion: 7, Active: true, CreatedAt: now.Add(-2 * time.Hour)}
	secrets, _ := json.Marshal(authstate.Secrets{AccessKey: "fixture", SecretKey: "upstream-secret", SessionToken: "upstream-session-token"})
	var err error
	c.Cipher, err = fixtureBox(t).Seal(secrets, authstate.NewConnection(c).AAD())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(c)
	exec(t, r, "INSERT INTO api_connections(id,owner,payload) VALUES(?,?,?)", c.ID, owner, string(raw))
	for _, mode := range []string{"bearer", "cookie", "revoked", "expired"} {
		a := authstate.AccessData{ID: mode, ConnectionID: c.ID, Owner: owner, Mode: "bearer", Revision: 9, Generation: 4, Active: true, CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(time.Hour), Digest: authstate.Digest(mode + "-token"), Policy: authstate.Policy{Scopes: []string{"objects:read", "buckets:read"}, Resources: []authstate.Resource{{Bucket: "reports", Prefix: "daily/"}}}}
		switch mode {
		case "bearer":
			a.PreviousDigest = authstate.Digest("previous-token")
			a.PreviousUntil = now.Add(time.Minute)
		case "cookie":
			a.Mode = "cookie"
			a.CSRFDigest = authstate.Digest("fixture-csrf")
		case "revoked":
			a.Active = false
		case "expired":
			a.ExpiresAt = now.Add(-time.Second)
		}
		if !authstate.ValidAccess(a) {
			t.Fatal("invalid fixture")
		}
		raw, _ := json.Marshal(a)
		exec(t, r, "INSERT INTO api_accesses(id,owner,connection_id,payload) VALUES(?,?,?,?)", a.ID, owner, c.ID, string(raw))
	}
	e := authstate.Event{ID: "legacy-event", Owner: owner, ActorID: "bearer", TargetID: "bearer", Action: "access.rotated", At: now.Add(-time.Minute)}
	raw, _ = json.Marshal(e)
	exec(t, r, "INSERT INTO api_events(id,owner,at_us,payload) VALUES(?,?,?,?)", e.ID, owner, e.At.UnixMicro(), string(raw))
}

func verifyAuthentication(t *testing.T, r *Runner) {
	t.Helper()
	var raw string
	if err := r.db.QueryRow("SELECT payload FROM api_connections").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var c authstate.ConnectionData
	if json.Unmarshal([]byte(raw), &c) != nil || !authstate.ValidConnection(c) {
		t.Fatal("connection changed")
	}
	plain, err := fixtureBox(t).Open(c.Cipher, authstate.NewConnection(c).AAD())
	if err != nil {
		t.Fatal("upstream credentials no longer decrypt", err)
	}
	var secrets authstate.Secrets
	if json.Unmarshal(plain, &secrets) != nil || secrets.SecretKey != "upstream-secret" || secrets.SessionToken != "upstream-session-token" {
		t.Fatal("upstream credentials changed")
	}
	rows, err := r.db.Query("SELECT payload FROM api_accesses ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		if rows.Scan(&raw) != nil {
			t.Fatal("access scan")
		}
		var a authstate.AccessData
		if json.Unmarshal([]byte(raw), &a) != nil || !authstate.ValidAccess(a) {
			t.Fatal("invalid preserved access")
		}
		access := authstate.NewAccess(a)
		valid, _ := access.Match(a.ID+"-token", time.Now())
		if valid != (a.ID == "bearer" || a.ID == "cookie") {
			t.Fatal("expiry or revocation changed", a.ID)
		}
		if a.ID == "bearer" {
			if ok, old := access.Match("previous-token", time.Now()); !ok || !old {
				t.Fatal("rotation grace changed")
			}
		}
		if a.ID == "cookie" && a.CSRFDigest != authstate.Digest("fixture-csrf") {
			t.Fatal("CSRF hash changed")
		}
		if !a.Policy.AllowsObject("reports", "daily/report.csv") || a.Policy.AllowsObject("reports", "outside/report.csv") || a.Policy.Allows("objects:write") {
			t.Fatal("API restrictions changed")
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
