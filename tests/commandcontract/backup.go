package commandcontract

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/commandlog"
)

// Backup checks the same restore and encoded-size boundaries in memory and SQL.
func Backup(t *testing.T, source, target commandlog.Repository) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	scope := commandlog.Scope{ActorID: "backup-actor", ConnectionID: "backup-connection", Operation: "copy"}
	entry, _, err := source.Reserve(ctx, scope, hash("backup-key"), hash("backup-body"), now)
	if err != nil {
		t.Fatal(err)
	}
	// This payload fits exactly before HTML escaping and grows to almost 384 KiB
	// when nested into a default JSON encoder's backup or SQL envelope.
	payload := json.RawMessage(`"` + strings.Repeat("<", commandlog.MaxResponseBytes-2) + `"`)
	if err = source.Finish(ctx, entry.ID, scope, &commandlog.Response{Status: 200, Payload: payload}); err != nil {
		t.Fatal("maximum valid response rejected", err)
	}
	entries, err := source.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	var imported []commandlog.Entry
	if err = json.Unmarshal(raw, &imported); err != nil {
		t.Fatal(err)
	}
	if err = target.Import(ctx, imported); err != nil {
		t.Fatal("escaped maximum response cannot restore", err)
	}
	if err = target.Import(ctx, entries); err != nil {
		t.Fatal("same offline import cannot resume", err)
	}
	replay, fresh, err := target.Reserve(ctx, scope, hash("backup-key"), hash("backup-body"), now)
	if err != nil || fresh || replay.ID != entry.ID || replay.Response == nil {
		t.Fatal("restored command executed twice", fresh, err)
	}
	var actual string
	if json.Unmarshal(replay.Response.Payload, &actual) != nil || actual != strings.Repeat("<", commandlog.MaxResponseBytes-2) {
		t.Fatal("response changed in backup")
	}
	invalid := append([]commandlog.Entry(nil), imported...)
	invalid = append(invalid, commandlog.Entry{})
	if err = target.Import(ctx, invalid); !errors.Is(err, commandlog.ErrInvalid) {
		t.Fatal("invalid full batch accepted", err)
	}
	conflict := append([]commandlog.Entry(nil), imported...)
	conflict[0] = conflict[0].Clone()
	conflict[0].Fingerprint = hash("changed request")
	if err = target.Import(ctx, conflict); !errors.Is(err, commandlog.ErrConflict) {
		t.Fatal("restore overwrote existing fence", err)
	}
	after, err := target.Export(ctx)
	if err != nil || len(after) != len(imported) || !commandlog.SameEntry(after[0], imported[0]) {
		t.Fatal("failed import mutated snapshot", err)
	}
	after[0].Response.Payload[0] = 'x'
	intact, _, err := target.Reserve(ctx, scope, hash("backup-key"), hash("backup-body"), now)
	if err != nil || !json.Valid(intact.Response.Payload) {
		t.Fatal("exported payload aliases repository", err)
	}
}
