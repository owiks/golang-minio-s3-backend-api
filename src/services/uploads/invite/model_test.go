package uploadinvite

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"minio-web/api/src/services/apperror"
)

func TestWriteReservationLimitAndExactlyOnceCompletion(t *testing.T) {
	invite := New(Config{Owner: "private-owner", Token: "token", Expires: time.Now().Add(time.Hour), MaxFiles: 3})
	var wg sync.WaitGroup
	var accepted atomic.Int32
	leases := make(chan *WriteLease, 32)
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lease, err := invite.BeginWrite(context.Background())
			if err != nil {
				if apperror.Public(err).Code != "InviteLimit" {
					t.Error(err)
				}
				return
			}
			accepted.Add(1)
			leases <- lease
		}()
	}
	wg.Wait()
	close(leases)
	if accepted.Load() != 3 {
		t.Fatal("concurrent reservation exceeded limit", accepted.Load())
	}
	for lease := range leases {
		for range 10 {
			wg.Add(1)
			go func() { defer wg.Done(); lease.Commit() }()
		}
	}
	wg.Wait()
	if invite.Snapshot().Uploaded != 3 || len(invite.active) != 0 {
		t.Fatal("completion counted more than once")
	}
	if _, err := invite.BeginWrite(context.Background()); err == nil {
		t.Fatal("completed limit ignored")
	}
	raw, err := json.Marshal(invite.Snapshot())
	if err != nil || strings.Contains(string(raw), "private-owner") || strings.Contains(string(raw), "active") {
		t.Fatal("private state serialized", err)
	}
}
func TestWriteRollbackAndRevocation(t *testing.T) {
	invite := New(Config{Expires: time.Now().Add(time.Hour), MaxFiles: 1})
	lease, err := invite.BeginWrite(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lease.Close()
	lease.Close()
	lease.Commit()
	if invite.Snapshot().Uploaded != 0 || len(invite.active) != 0 {
		t.Fatal("failed reservation counted or retained")
	}
	active, err := invite.BeginWrite(context.Background())
	if err != nil {
		t.Fatal("rollback did not free capacity", err)
	}
	invite.Stop()
	if active.Context().Err() != context.Canceled {
		t.Fatal("revoke did not cancel writer")
	}
	active.Close()
	if _, err := invite.BeginWrite(context.Background()); err == nil || apperror.Public(err).Code != "InviteExpired" {
		t.Fatal("revoked writer admitted", err)
	}
	expired := New(Config{Expires: time.Now().Add(-time.Second), MaxFiles: 1})
	if _, err := expired.BeginWrite(context.Background()); err == nil {
		t.Fatal("expired reservation admitted")
	}
}
func TestReserveRacingWithRevokeCannotLeaveLiveWrite(t *testing.T) {
	for range 40 {
		invite := New(Config{Expires: time.Now().Add(time.Hour), MaxFiles: 1})
		var wg sync.WaitGroup
		var lease *WriteLease
		wg.Add(2)
		go func() { defer wg.Done(); lease, _ = invite.BeginWrite(context.Background()) }()
		go func() { defer wg.Done(); invite.Stop() }()
		wg.Wait()
		if lease != nil {
			if lease.Context().Err() != context.Canceled {
				t.Fatal("writer escaped revocation")
			}
			lease.Close()
		}
		if len(invite.active) != 0 {
			t.Fatal("active write leaked")
		}
	}
}
