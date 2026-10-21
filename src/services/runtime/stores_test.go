package runtime

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"minio-web/api/src/config"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/archives/plan"
	"minio-web/api/src/services/folders/share"
	"minio-web/api/src/services/staging"
	"minio-web/api/src/services/uploads/invite"
)

func TestStoresRejectPublicationAfterClose(t *testing.T) {
	app := New(config.Default())
	g := foldershare.New(foldershare.Config{Token: "share", Owner: "owner", Prefix: "folder/", Expires: time.Now().Add(time.Hour)})
	if err := app.Shares.Add(g); err != nil {
		t.Fatal(err)
	}
	app.Close()
	app.Close()
	checks := map[string]func() error{
		"session": func() error { return app.StoreSession(&Session{id: "late"}) },
		"archive": func() error {
			_, err := app.Archives.Put(context.Background(), archiveplan.New(archiveplan.Config{SessionID: "late", Created: time.Now()}))
			return err
		},
		"invite": func() error { return app.Invites.Add(uploadinvite.New(uploadinvite.Config{Token: "late"})) },
		"share":  func() error { return app.Shares.Add(foldershare.New(foldershare.Config{Token: "late"})) },
		"ticket": func() error { _, err := app.Shares.IssueTicket(g, "folder/file.txt", "download"); return err },
		"zip": func() error {
			_, err := app.ZIPs.Open(context.Background(), nil, "bucket", "file.zip")
			return err
		},
	}
	for name, check := range checks {
		t.Run(name, func(t *testing.T) {
			if err := check(); err == nil || apperror.Public(err).Code != "ServerClosing" {
				t.Fatalf("late publication: %v", err)
			}
		})
	}
	if raw, err := json.Marshal(app); err != nil || string(raw) != "{}" {
		t.Fatalf("private state serialized: %s %v", raw, err)
	}
}

func TestArchivePublicationLimitIsAtomicAndSessionBound(t *testing.T) {
	app := New(config.Default())
	defer app.Close()
	if err := app.StoreSession(&Session{id: "owner", expires: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	var accepted atomic.Int32
	var workers sync.WaitGroup
	for range 24 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := app.Archives.Put(context.Background(), archiveplan.New(archiveplan.Config{SessionID: "owner", Created: time.Now()}))
			if err == nil {
				accepted.Add(1)
			} else if apperror.Public(err).Code != "ArchiveLimit" {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	workers.Wait()
	if accepted.Load() != 8 {
		t.Fatalf("per-session bound lost: %d", accepted.Load())
	}
	app.RemoveSession("owner")
	if _, err := app.Archives.Put(context.Background(), archiveplan.New(archiveplan.Config{SessionID: "owner", Created: time.Now()})); err == nil || apperror.Public(err).Code != "SessionExpired" {
		t.Fatalf("published after logout: %v", err)
	}
}

func TestStoreReapingPreservesDelegatedLifetimes(t *testing.T) {
	app := New(config.Default())
	defer app.Close()
	now := time.Now()
	if err := app.StoreSession(&Session{id: "owner", expires: now.Add(-time.Second)}); err != nil {
		t.Fatal(err)
	}
	plan := archiveplan.New(archiveplan.Config{SessionID: "owner", Created: now})
	ticket, err := app.Archives.Put(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	share := foldershare.New(foldershare.Config{Owner: "owner", Token: "share", Expires: now.Add(time.Hour)})
	invite := uploadinvite.New(uploadinvite.Config{Owner: "owner", Token: "invite", Expires: now.Add(time.Hour), MaxFiles: 1})
	shareCtx, release, err := share.BeginRead(context.Background(), app)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	lease, err := invite.BeginWrite(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	inviteCtx := lease.Context()
	if err := app.Shares.Add(share); err != nil {
		t.Fatal(err)
	}
	if err := app.Invites.Add(invite); err != nil {
		t.Fatal(err)
	}
	app.reap(now)
	if app.Session("owner") != nil || app.Archives.Get(ticket, "owner") != nil || !plan.Snapshot("").Cancelled {
		t.Fatal("expired session or archive retained")
	}
	if app.Shares.Get("share") == nil || app.Invites.Get("invite") == nil || shareCtx.Err() != nil || inviteCtx.Err() != nil {
		t.Fatal("owner expiry shortened delegated access")
	}
	app.reap(now.Add(2 * time.Hour))
	if app.Shares.Get("share") != nil || app.Invites.Get("invite") != nil || shareCtx.Err() == nil || inviteCtx.Err() == nil {
		t.Fatal("expired grants or active operations retained")
	}
}

func TestShareTicketsAreSingleUseAndRevocationIsAtomic(t *testing.T) {
	store := NewFolderShareStore()
	defer store.Close()
	g := foldershare.New(foldershare.Config{Token: "share", Owner: "owner", Prefix: "folder/", Expires: time.Now().Add(500 * time.Millisecond)})
	if err := store.Add(g); err != nil {
		t.Fatal(err)
	}
	ticket, err := store.IssueTicket(g, "folder/file.txt", "download")
	if err != nil {
		t.Fatal(err)
	}
	preview, ok := store.TakeTicket(ticket, false)
	if !ok || !preview.Expires.Equal(g.Expires()) {
		t.Fatal("HEAD consumed ticket or expiry precision lost")
	}
	var consumed atomic.Int32
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := store.TakeTicket(ticket, true); ok {
				consumed.Add(1)
			}
		}()
	}
	wg.Wait()
	if consumed.Load() != 1 {
		t.Fatal("ticket consumed more than once", consumed.Load())
	}
	if err := store.Revoke("other", "share"); err == nil {
		t.Fatal("another owner revoked share")
	}
	issued, err := store.IssueTicket(g, "folder/file.txt", "media")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke("owner", "share"); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.TakeTicket(issued, true); ok {
		t.Fatal("revocation retained ticket")
	}
	if _, err := store.IssueTicket(g, "folder/file.txt", "download"); err == nil {
		t.Fatal("ticket created for a revoked grant")
	}
}

func TestShareIssueRacingWithRevokeLeavesNoTicket(t *testing.T) {
	for range 30 {
		store := NewFolderShareStore()
		g := foldershare.New(foldershare.Config{Token: "share", Owner: "owner", Prefix: "folder/", Expires: time.Now().Add(time.Hour)})
		if err := store.Add(g); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _, _ = store.IssueTicket(g, "folder/file.txt", "download") }()
		go func() {
			defer wg.Done()
			if err := store.Revoke("owner", "share"); err != nil {
				t.Error(err)
			}
		}()
		wg.Wait()
		if len(store.tickets) != 0 {
			t.Fatal("ticket survived revoke")
		}
		store.Close()
	}
}

func TestInviteSnapshotsDoNotAliasLiveCounters(t *testing.T) {
	store := NewUploadInviteStore()
	defer store.Close()
	g := uploadinvite.New(uploadinvite.Config{Token: "invite", Owner: "owner", Bucket: "bucket", Expires: time.Now().Add(time.Hour)})
	if err := store.Add(g); err != nil {
		t.Fatal(err)
	}
	rows := store.List("owner", "bucket", "")
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	rows[0].Uploaded = 999
	if got := store.List("owner", "bucket", ""); got[0].Uploaded != 0 {
		t.Fatal("snapshot aliases live invite")
	}
	if err := store.Revoke("other", "invite"); err == nil || store.Get("invite") == nil {
		t.Fatal("foreign owner altered invitation")
	}
}

func TestSeparateStoresDoNotShareCollectionLock(t *testing.T) {
	app := New(config.Default())
	defer app.Close()
	app.Invites.mu.Lock()
	done := make(chan struct{})
	go func() { _ = app.Session("missing"); _ = app.Shares.List("owner", "bucket", ""); close(done) }()
	select {
	case <-done:
		app.Invites.mu.Unlock()
	case <-time.After(time.Second):
		app.Invites.mu.Unlock()
		t.Fatal("unrelated stores blocked by invite collection lock")
	}
}

func TestZIPCacheRetainsActiveFileUntilFinalRelease(t *testing.T) {
	for _, reason := range []string{"shutdown", "session-removed"} {
		t.Run(reason, func(t *testing.T) {
			budget := staging.New(4)
			file, err := budget.Create(4, "minio-store-test-*")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if _, err := file.Write([]byte("body")); err != nil {
				t.Fatal(err)
			}
			alive := true
			cache := NewZIPCache(budget, func(string) bool { return alive })
			entry := &storedZIP{File: file, SessionID: "owner", Refs: 1, Used: time.Now()}
			cache.entries["entry"] = entry
			release := cache.release("entry", entry)
			if reason == "shutdown" {
				cache.Close()
			} else {
				alive = false
				cache.Reap(time.Now())
			}
			buf := make([]byte, 4)
			if n, err := file.ReadAt(buf, 0); err != nil || n != 4 || string(buf) != "body" || budget.Used() != 4 {
				t.Fatal("active ZIP closed", n, err)
			}
			release()
			release()
			if budget.Used() != 0 || len(cache.entries) != 0 {
				t.Fatal("ZIP reservation leaked")
			}
		})
	}
}

func TestConcurrentPublicationAndCloseLeavesNoResources(t *testing.T) {
	app := New(config.Default())
	var wg sync.WaitGroup
	for i := range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token := strconv.Itoa(i)
			g := uploadinvite.New(uploadinvite.Config{Token: token, Expires: time.Now().Add(time.Hour)})
			if err := app.Invites.Add(g); err != nil && apperror.Public(err).Code != "ServerClosing" {
				t.Error(err)
			}
			_ = app.Shares.Add(foldershare.New(foldershare.Config{Token: token, Expires: time.Now().Add(time.Hour)}))
		}()
	}
	wg.Add(1)
	go func() { defer wg.Done(); app.Close() }()
	wg.Wait()
	app.Close()
	if len(app.Invites.List("", "", "")) != 0 || len(app.Shares.List("", "", "")) != 0 {
		t.Fatal("late grants survived shutdown")
	}
}
