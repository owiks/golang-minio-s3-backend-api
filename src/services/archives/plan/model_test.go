package archiveplan

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/objects/state"
)

func TestArchiveSnapshotDoesNotAliasPlanOrExposeConditions(t *testing.T) {
	config := Config{Created: time.Now(), Items: []ArchiveItem{{Key: "file", Size: 4, Sent: 2, State: "pending", Info: objectstate.DeletionSnapshot{ETag: "private-condition"}}, {Key: "unavailable", State: "pending"}}, Issues: map[string]string{"missing": "not found"}, AllowPartial: true}
	plan := New(config)
	config.Items[0].Key = "constructor-alias"
	config.Issues["missing"] = "constructor-alias"
	snapshot := plan.Snapshot("ticket")
	if snapshot.Count != 1 || snapshot.Size != 4 || snapshot.Sent != 2 || !snapshot.ExpiresAt.Equal(config.Created.Add(time.Hour)) {
		t.Fatal("snapshot totals changed")
	}
	snapshot.Items[0].Key = "changed"
	snapshot.Issues["missing"] = "changed"
	actual := plan.Snapshot("ticket")
	if actual.Items[0].Key != "file" || actual.Issues["missing"] != "not found" {
		t.Fatal("constructor or snapshot aliases plan")
	}
	raw, err := json.Marshal(snapshot)
	if err != nil || strings.Contains(string(raw), "private-condition") || strings.Contains(string(raw), "Cancel") {
		t.Fatal("private state serialized", err)
	}
	ctx, _, cancel, err := plan.BeginDownload(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	plan.Abort()
	if !plan.Snapshot("").Cancelled || ctx.Err() != context.Canceled {
		t.Fatal("detached work not cancelled")
	}
}
func TestConcurrentArchiveStartAndProgress(t *testing.T) {
	plan := New(Config{Items: []ArchiveItem{{Key: "file", State: "pending"}}})
	var started atomic.Int32
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, snapshot, cancel, err := plan.BeginDownload(context.Background())
			if err != nil {
				if apperror.Public(err).Code != "ArchiveStarted" {
					t.Error(err)
				}
				return
			}
			defer cancel()
			started.Add(1)
			snapshot.Items[0].Key = "changed"
		}()
	}
	wg.Wait()
	if started.Load() != 1 || plan.Snapshot("").Items[0].Key != "file" {
		t.Fatal("start not exclusive or snapshot not owned")
	}
	if _, err := plan.Configure("ticket", "new", "", false); err == nil || apperror.Public(err).Code != "ArchiveStarted" {
		t.Fatal("running plan reconfigured")
	}
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				plan.AddSent(0, 1)
				_ = plan.Snapshot("")
			}
		}()
	}
	wg.Wait()
	plan.CompleteItem(0)
	outcome, code := plan.Finish(nil, true)
	if outcome != "success" || code != "" || plan.Snapshot("").Sent != 1600 {
		t.Fatal("progress/completion lost", outcome, code)
	}
	plan.Cancel("ticket")
	plan.FailItem(0, "late")
	plan.AddSent(0, 10)
	if snap := plan.Snapshot(""); !snap.Finished || snap.Cancelled || snap.Items[0].State != "done" || snap.Sent != 1600 {
		t.Fatal("late mutation changed completed plan", snap)
	}
	if outcome, code := plan.Finish(context.Canceled, false); outcome != "success" || code != "" {
		t.Fatal("completion not idempotent")
	}
}
func TestCancelAndCompletionHaveOneSerializedOutcome(t *testing.T) {
	for range 40 {
		plan := New(Config{Items: []ArchiveItem{{Key: "file", State: "pending"}}})
		ctx, _, cancel, err := plan.BeginDownload(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); plan.Cancel("ticket") }()
		go func() { defer wg.Done(); plan.CompleteItem(0); plan.Finish(nil, true) }()
		wg.Wait()
		snap := plan.Snapshot("ticket")
		if !snap.Finished {
			t.Fatal("completion lost")
		}
		if snap.Cancelled && ctx.Err() != context.Canceled {
			t.Fatal("cancelled stream still active")
		}
		outcome, _ := plan.Finish(nil, true)
		if snap.Cancelled != (outcome == "cancelled") {
			t.Fatal("outcome contradicts cancellation", outcome, snap)
		}
		cancel()
	}
}
