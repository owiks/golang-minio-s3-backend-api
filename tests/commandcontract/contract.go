// Package commandcontract verifies identical execution fences for memory and SQL.
package commandcontract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/commandlog"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
)

func hash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func Run(t *testing.T, repository commandlog.Repository) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	scope := func() commandlog.Scope {
		return commandlog.Scope{ActorID: securetoken.New(), ConnectionID: securetoken.New(), Operation: "POST objects/transfer"}
	}

	t.Run("reserve replay and ownership", func(t *testing.T) {
		s := scope()
		entry, fresh, err := repository.Reserve(ctx, s, hash("key"), hash("body"), now)
		if err != nil || !fresh || entry.State != commandlog.Pending || !commandlog.ValidEntry(entry) {
			t.Fatal("cannot reserve", fresh, entry.State, err)
		}
		if _, fresh, err = repository.Reserve(ctx, s, hash("key"), hash("body"), now); !errors.Is(err, commandlog.ErrInProgress) || fresh {
			t.Fatal("pending command executed twice", fresh, err)
		}
		if _, fresh, err = repository.Reserve(ctx, s, hash("key"), hash("different"), now); !errors.Is(err, commandlog.ErrConflict) || fresh {
			t.Fatal("key accepted different request", fresh, err)
		}
		other := s
		other.ActorID = securetoken.New()
		if err = repository.Finish(ctx, entry.ID, other, nil); !errors.Is(err, commandlog.ErrNotFound) {
			t.Fatal("other actor changed command", err)
		}
		response := commandlog.Response{Status: 200, Payload: json.RawMessage(`{"copied":true,"moved":false}`), MessageID: "transfers.copyCreated"}
		if err = repository.Finish(ctx, entry.ID, s, &response); err != nil {
			t.Fatal(err)
		}
		response.Payload[2] = 'X'
		replay, fresh, err := repository.Reserve(ctx, s, hash("key"), hash("body"), now)
		if err != nil || fresh || replay.ID != entry.ID || replay.State != commandlog.Completed || string(replay.Response.Payload) != `{"copied":true,"moved":false}` {
			t.Fatal("completed result changed", fresh, replay, err)
		}
		replay.Response.Payload[2] = 'Y'
		again, _, err := repository.Reserve(ctx, s, hash("key"), hash("body"), now)
		if err != nil || string(again.Response.Payload) != `{"copied":true,"moved":false}` {
			t.Fatal("returned result aliases repository", err)
		}
		if err = repository.Finish(ctx, entry.ID, s, nil); !errors.Is(err, commandlog.ErrConflict) {
			t.Fatal("completed result overwritten", err)
		}
		for _, changed := range []commandlog.Scope{other, {ActorID: s.ActorID, ConnectionID: securetoken.New(), Operation: s.Operation}, {ActorID: s.ActorID, ConnectionID: s.ConnectionID, Operation: "POST another"}} {
			if _, fresh, err := repository.Reserve(ctx, changed, hash("key"), hash("body"), now); err != nil || !fresh {
				t.Fatal("unrelated scope collided", fresh, err)
			}
		}
	})

	t.Run("atomic reservation", func(t *testing.T) {
		s := scope()
		var freshCount atomic.Int64
		var wg sync.WaitGroup
		for range 24 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, fresh, err := repository.Reserve(ctx, s, hash("same"), hash("same request"), now)
				if fresh && err == nil {
					freshCount.Add(1)
				} else if fresh || !errors.Is(err, commandlog.ErrInProgress) {
					t.Error("unexpected concurrent reservation", fresh, err)
				}
			}()
		}
		wg.Wait()
		if freshCount.Load() != 1 {
			t.Fatal("command has multiple owners", freshCount.Load())
		}
	})

	t.Run("uncertain lease and explicit uncertain outcome", func(t *testing.T) {
		for _, expire := range []bool{true, false} {
			s := scope()
			entry, _, err := repository.Reserve(ctx, s, hash("unknown"), hash("request"), now)
			if err != nil {
				t.Fatal(err)
			}
			at := now
			if expire {
				at = now.Add(commandlog.LeaseTTL)
			} else if err = repository.Finish(ctx, entry.ID, s, nil); err != nil {
				t.Fatal(err)
			}
			replay, fresh, err := repository.Reserve(ctx, s, hash("unknown"), hash("request"), at)
			if fresh || !errors.Is(err, commandlog.ErrUnknown) || replay.State != commandlog.Unknown {
				t.Fatal("uncertain command became executable", fresh, replay.State, err)
			}
			if err := repository.Finish(ctx, entry.ID, s, &commandlog.Response{Status: 200}); !errors.Is(err, commandlog.ErrConflict) {
				t.Fatal("late worker replaced unknown outcome", err)
			}
		}
	})

	t.Run("validate and bound responses", func(t *testing.T) {
		s := scope()
		if _, _, err := repository.Reserve(ctx, s, "raw client key", hash("body"), now); !errors.Is(err, commandlog.ErrInvalid) {
			t.Fatal("unhashed key accepted", err)
		}
		entry, _, err := repository.Reserve(ctx, s, hash("bounded"), hash("body"), now)
		if err != nil {
			t.Fatal(err)
		}
		for _, response := range []commandlog.Response{{Status: 200, Payload: json.RawMessage(`broken`)}, {Status: 200, Payload: json.RawMessage(`"` + strings.Repeat("a", commandlog.MaxResponseBytes) + `"`)}, {Status: 500}, {Status: 200, MessageID: "bad\nmessage"}} {
			if err := repository.Finish(ctx, entry.ID, s, &response); !errors.Is(err, commandlog.ErrInvalid) {
				t.Fatal("invalid result accepted", err)
			}
		}
		if _, _, err := repository.Reserve(ctx, s, hash("bounded"), hash("body"), now); !errors.Is(err, commandlog.ErrInProgress) {
			t.Fatal("invalid finish changed pending command", err)
		}
	})

	t.Run("actor limit never evicts active records", func(t *testing.T) {
		s := scope()
		for i := range commandlog.MaxPerActor {
			if _, fresh, err := repository.Reserve(ctx, s, hash(fmt.Sprint(i)), hash("body"), now); err != nil || !fresh {
				t.Fatal(i, err)
			}
		}
		if _, _, err := repository.Reserve(ctx, s, hash("overflow"), hash("body"), now); !errors.Is(err, commandlog.ErrLimit) {
			t.Fatal("actor limit ignored", err)
		}
		otherConnection := s
		otherConnection.ConnectionID = securetoken.New()
		if _, _, err := repository.Reserve(ctx, otherConnection, hash("overflow"), hash("body"), now); !errors.Is(err, commandlog.ErrLimit) {
			t.Fatal("actor limit bypassed through another connection", err)
		}
		if _, _, err := repository.Reserve(ctx, s, hash("0"), hash("body"), now); !errors.Is(err, commandlog.ErrInProgress) {
			t.Fatal("limit evicted command fence", err)
		}
	})

	t.Run("retention and stale completion", func(t *testing.T) {
		s := scope()
		before, _, err := repository.Reserve(ctx, s, hash("expiry"), hash("old"), now)
		if err != nil {
			t.Fatal(err)
		}
		after, fresh, err := repository.Reserve(ctx, s, hash("expiry"), hash("new"), now.Add(commandlog.Retention))
		if err != nil || !fresh || after.ID == before.ID {
			t.Fatal("expired fence not replaced", fresh, err)
		}
		if err := repository.Finish(ctx, before.ID, s, nil); !errors.Is(err, commandlog.ErrNotFound) {
			t.Fatal("stale worker completed replacement", err)
		}
	})
}
