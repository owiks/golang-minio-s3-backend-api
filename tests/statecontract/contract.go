// Package statecontract exercises the same observable contract for every adapter.
package statecontract

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/secretbox"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
)

func Run(t *testing.T, repo authstate.Repository) {
	t.Helper()
	ctx := context.Background()
	box, err := secretbox.Ephemeral()
	if err != nil {
		t.Fatal(err)
	}
	m := authstate.NewManager(repo, box, time.Hour).WithVerifier(nativeProof{})
	id, cid, secret := securetoken.New(), securetoken.New(), securetoken.New()
	boot := authstate.Bootstrap{ID: id, ConnectionID: cid, Endpoint: "https://s3.example.test", Region: "us-east-1", Mode: "bearer", Digest: authstate.Digest(secret), ExpiresAt: time.Now().Add(time.Hour), Secrets: authstate.Secrets{AccessKey: "fixture-key", SecretKey: "fixture-secret"}}
	if err = m.Bootstrap(ctx, boot); err != nil {
		t.Fatal(err)
	}
	a, c, err := m.Lookup(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	g := authstate.CredentialGuard(a, false)
	if raw, e := m.Decrypt(c); e != nil || raw.SecretKey != "fixture-secret" {
		t.Fatal("cannot decrypt", e)
	}
	if err = m.Bootstrap(ctx, boot); apperror.Public(err).Code != "AccessStateConflict" {
		t.Fatal("duplicate accepted", err)
	}
	p := authstate.Policy{Scopes: []string{"objects:read", "buckets:read"}, Resources: []authstate.Resource{{Bucket: "data", Prefix: "reports/"}}}
	child, err := m.Issue(ctx, g, cid, authstate.IssueRequest{Name: "report reader", TTLSeconds: 600, Policy: p})
	if err != nil {
		t.Fatal(err)
	}
	p.Resources[0].Prefix = "outside/"
	ca, _, err := m.Lookup(ctx, child.ID)
	if err != nil || !ca.Snapshot().Policy.AllowsObject("data", "reports/file") {
		t.Fatal("mutable state leaked", err)
	}
	if ok, prev := ca.Match(child.Token, time.Now()); !ok || prev {
		t.Fatal("credential hash mismatch")
	}
	raw, _ := json.Marshal(ca.View())
	if strings.Contains(string(raw), child.Token) || strings.Contains(string(raw), authstate.Digest(child.Token)) {
		t.Fatal("hash/secret exposed")
	}
	if _, err = m.Issue(ctx, authstate.CredentialGuard(ca, false), cid, authstate.IssueRequest{TTLSeconds: 1, Policy: authstate.FullPolicy()}); apperror.Public(err).Status != 403 {
		t.Fatal("scope escalation accepted", err)
	}
	foreign := g
	foreign.Owner = authstate.Digest("different owner")
	if _, err = m.Connections(ctx, foreign); apperror.Public(err).Status != 403 {
		t.Fatal("owner check missing", err)
	}
	if _, err = m.Edit(ctx, g, child.ID, authstate.EditRequest{Revision: 1, Policy: ptr(authstate.FullPolicy())}); apperror.Public(err).Status != 403 {
		t.Fatal("policy expansion accepted", err)
	}
	// An audit write failure must roll back the access update as well.
	history, e := m.Events(ctx, g)
	if e != nil {
		t.Fatal(e)
	}
	duplicate := history[0]
	duplicate.ActorID = id
	duplicate.TargetID = child.ID
	duplicate.Action = "access.updated"
	duplicate.At = time.Now().UTC()
	name := "must roll back"
	if _, e = repo.Apply(ctx, authstate.Command{Kind: "edit", Guard: g, TargetID: child.ID, Revision: 1, Name: &name, Event: duplicate}); e == nil {
		t.Fatal("duplicate audit ID accepted")
	}
	unchanged, _, e := m.Lookup(ctx, child.ID)
	if e != nil || unchanged.Snapshot().Revision != 1 || unchanged.Snapshot().Name != ca.Snapshot().Name {
		t.Fatal("failed audit published access change", e)
	}
	// Concurrent compare-and-swap: exactly one secret is committed and audit row
	// is committed in the same transaction. The caller remains unchanged.
	var won atomic.Int32
	var winnersMu sync.Mutex
	var winner authstate.Issued
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			v, e := m.Rotate(ctx, g, child.ID, authstate.RotateRequest{Revision: 1, GraceSeconds: 60})
			if e == nil {
				won.Add(1)
				winnersMu.Lock()
				winner = v
				winnersMu.Unlock()
			} else if apperror.Public(e).Code != "AccessStateConflict" {
				t.Error("unexpected CAS result", e)
			}
		})
	}
	wg.Wait()
	if won.Load() != 1 {
		t.Fatal("rotation not atomic", won.Load())
	}
	ca, _, err = m.Lookup(ctx, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ok, prev := ca.Match(child.Token, time.Now()); !ok || !prev {
		t.Fatal("rotation overlap missing")
	}
	if ok, prev := ca.Match(winner.Token, time.Now()); !ok || prev {
		t.Fatal("new token not current")
	}
	if _, err = m.Connections(ctx, authstate.CredentialGuard(a, true)); apperror.Public(err).Status != 403 {
		t.Fatal("previous secret manages access", err)
	}
	if _, err = m.Renew(ctx, g, child.ID, authstate.RenewRequest{Revision: 2, TTLSeconds: 900}); err != nil {
		t.Fatal(err)
	}
	ca, _, _ = m.Lookup(ctx, child.ID)
	if ca.Snapshot().ExpiresAt.After(a.Snapshot().ExpiresAt) {
		t.Fatal("child outlives issuer")
	}
	if err = m.Touch(ctx, child.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = m.Revoke(ctx, g, child.ID, 3); err != nil {
		t.Fatal(err)
	}
	ca, _, _ = m.Lookup(ctx, child.ID)
	if ca.Valid(time.Now()) {
		t.Fatal("revocation not visible")
	}
	if _, err = m.Rotate(ctx, g, child.ID, authstate.RotateRequest{Revision: 4}); apperror.Public(err).Status != 409 {
		t.Fatal("revoked token rotated", err)
	}
	if _, err = m.Renew(ctx, g, child.ID, authstate.RenewRequest{Revision: 4, TTLSeconds: 60}); apperror.Public(err).Status != 409 {
		t.Fatal("revoked token renewed", err)
	}
	events, err := m.Events(ctx, g)
	if err != nil || len(events) != 5 {
		t.Fatal("audit missing or failed CAS audited", len(events), err)
	}
	backup, err := m.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(backup)
	if strings.Contains(string(encoded), "fixture-secret") || strings.Contains(string(encoded), child.Token) || strings.Contains(string(encoded), secret) {
		t.Fatal("database contains plaintext secret")
	}
	restoreRepo := authstate.NewMemory()
	restore := authstate.NewManager(restoreRepo, box, time.Hour)
	defer restore.Close()
	if err = restore.Restore(ctx, backup, false); err != nil {
		t.Fatal(err)
	}
	if !backup.Accesses[0].Active && backup.Accesses[0].ID == id {
		t.Fatal("restore mutated caller backup")
	}
	restored, _, _ := restore.Lookup(ctx, id)
	if restored.Valid(time.Now()) {
		t.Fatal("backup restored a live credential")
	}
	transferRepo := authstate.NewMemory()
	transfer := authstate.NewManager(transferRepo, box, time.Hour)
	defer transfer.Close()
	if err = transfer.Restore(ctx, backup, true); err != nil {
		t.Fatal(err)
	}
	transferred, _, _ := transfer.Lookup(ctx, id)
	if ok, _ := transferred.Match(secret, time.Now()); !ok {
		t.Fatal("transfer changed credential")
	}
	if err = transfer.Restore(ctx, backup, true); apperror.Public(err).Status != 409 {
		t.Fatal("restore overwrote destination", err)
	}
	if err = m.DeleteConnection(ctx, g, cid, 1); err != nil {
		t.Fatal(err)
	}
	root, conn, err := m.Lookup(ctx, id)
	if err != nil || root.Valid(time.Now()) || conn.Snapshot().Active || len(conn.Snapshot().Cipher.Data) > 0 {
		t.Fatal("connection revocation incomplete", err)
	}
	if err = m.Verify(ctx); err != nil {
		t.Fatal(err)
	}
	// Retention deletes only old expired records, never a live credential,
	// and its destructive boundary is validated by the adapter itself.
	past := time.Now().UTC().Add(-72 * time.Hour)
	cd := c.Snapshot()
	cd.ID = securetoken.New()
	cd.CreatedAt = past
	plaintext, _ := json.Marshal(authstate.Secrets{AccessKey: "fixture-key", SecretKey: "fixture-secret"})
	cd.Cipher, err = box.Seal(plaintext, authstate.NewConnection(cd).AAD())
	if err != nil {
		t.Fatal(err)
	}
	ad := a.Snapshot()
	ad.ID = securetoken.New()
	ad.ConnectionID = cd.ID
	ad.CreatedAt = past
	ad.ExpiresAt = past.Add(time.Hour)
	ev := authstate.Event{ID: securetoken.New(), Owner: cd.Owner, ActorID: ad.ID, TargetID: ad.ID, Action: "access.created", At: past}
	if err = repo.Bootstrap(ctx, authstate.NewConnection(cd), authstate.NewAccess(ad), ev); err != nil {
		t.Fatal(err)
	}
	if n, e := repo.Prune(ctx, time.Now().Add(-24*time.Hour)); e != nil || n != 2 {
		t.Fatal("expired records not pruned atomically", n, e)
	}
	if _, _, e := repo.Get(ctx, ad.ID); apperror.Public(e).Status != 404 {
		t.Fatal("expired access remains", e)
	}
	if _, e := repo.Prune(ctx, time.Now().Add(time.Hour)); apperror.Public(e).Status != 400 {
		t.Fatal("future prune accepted", e)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err = m.Lookup(cancelled, id); err == nil {
		t.Fatal("cancelled read succeeded")
	}
	runLifecycle(t, repo)
}
func ptr(p authstate.Policy) *authstate.Policy { return &p }

// The repository contract uses a trusted native verifier; HTTP/native suites
// exercise real MinIO proof, including principal deletion and key-name reuse.
type nativeProof struct{}

func (nativeProof) Verify(context.Context, authstate.ConnectionView, authstate.Secrets) error {
	return nil
}
