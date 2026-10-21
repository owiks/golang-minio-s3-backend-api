package objectstate

import (
	"testing"
	"time"
)

func TestSnapshotRevisionCompatibilityAndConditions(t *testing.T) {
	snapshot := DeletionSnapshot{ETag: "etag", Size: 9007199254740993, Modified: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC), Version: "v1"}
	rev := DeletionRevision(snapshot)
	if rev != "e3afc2b6ad7b3d930fc3e2a20a31e0283836303f1f70e412b33c0df8946d0a34" {
		t.Fatal("existing deletion revision changed", rev)
	}
	for _, field := range []string{"etag", "size", "modified", "version"} {
		t.Run(field, func(t *testing.T) {
			changed := snapshot
			switch field {
			case "etag":
				changed.ETag += "-other"
			case "size":
				changed.Size++
			case "modified":
				changed.Modified = changed.Modified.Add(time.Nanosecond)
			case "version":
				changed.Version += "-other"
			}
			if DeletionRevision(changed) == rev {
				t.Fatal("source condition omitted", field)
			}
		})
	}
}
