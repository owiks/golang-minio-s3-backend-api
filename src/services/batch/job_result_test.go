package batch

import (
	"encoding/json"
	"github.com/minio/madmin-go/v3"
	"testing"
	"time"
)

func TestJobDTOCompatibility(t *testing.T) {
	for _, value := range []madmin.BatchJobResult{{}, {ID: "job-id", Type: "replicate", User: "alice", Started: time.Unix(1700000000, 0).UTC(), Elapsed: time.Second}} {
		before, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		after, err := json.Marshal(jobResult(value))
		if err != nil {
			t.Fatal(err)
		}
		if string(before) != string(after) {
			t.Fatalf("batch contract changed:\n%s\n%s", before, after)
		}
	}
}
