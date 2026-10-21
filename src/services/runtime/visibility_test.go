package runtime

import (
	"encoding/json"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/archives/plan"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/objects/state"
)

func TestSessionAndPrivateSnapshotsNeverSerialize(t *testing.T) {
	value := Session{id: "private-id", token: "private-cookie", csrf: "private-csrf", actor: "private-user"}
	raw, err := json.Marshal(&value)
	if err != nil || string(raw) != "{}" {
		t.Fatalf("session internals became serializable: %s, %v", raw, err)
	}
	item := archiveplan.ArchiveItem{Key: "file.txt", Info: objectstate.DeletionSnapshot{ETag: "private-condition"}}
	var wire map[string]any
	raw, err = json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if _, exists := wire["Info"]; exists {
		t.Fatal("archive includes private deletion condition")
	}
	if _, exists := wire["info"]; exists {
		t.Fatal("archive includes private deletion condition")
	}
}
