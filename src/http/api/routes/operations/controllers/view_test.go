package controllers

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/openapi"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/operations"
)

func TestPublicOperationPhaseAndTimingRemainSafeAndDocumented(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s, err := operations.New(operations.Scope{ActorID: "private-actor", ConnectionID: "private-connection"}, operations.Spec{Scan: &operations.ScanSpec{Bucket: "data"}}, now.Add(time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	v := publicView(s, now.Add(3*time.Second))
	if v.Phase != "waiting" || v.WaitingReason != "scheduler" || v.Timing == nil || v.Timing.WaitingMS != 3000 || v.Timing.ExecutionMS != 0 || v.Timing.StartedAt != nil || v.Timing.FinishedAt != nil {
		t.Fatal(v)
	}
	raw, _ := json.Marshal(v)
	for _, hidden := range []string{"private-actor", "private-connection", "waitingSince", "executionSince", "waitingUS", "executionUS", "leaseUntil"} {
		if strings.Contains(string(raw), hidden) {
			t.Fatal("internal timing exposed", hidden, string(raw))
		}
	}
	s.Timing = nil
	raw, _ = json.Marshal(publicView(s, now))
	if strings.Contains(string(raw), `"timing"`) {
		t.Fatal("legacy timing should be unknown", string(raw))
	}
	schema := openapi.TypeSchema(View{}, false).Value
	for _, field := range []string{"phase", "waitingReason", "timing"} {
		if schema.Properties[field] == nil {
			t.Fatal("operation field missing from OpenAPI", field)
		}
	}
	for _, field := range []string{"startedAt", "finishedAt", "waitingMs", "executionMs"} {
		if schema.Properties["timing"].Value.Properties[field] == nil {
			t.Fatal("timing field missing from OpenAPI", field)
		}
	}
}

func TestPublicPauseExpiresWithoutInventingExecutionStart(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s, err := operations.New(operations.Scope{ActorID: "actor", ConnectionID: "connection"}, operations.Spec{Scan: &operations.ScanSpec{Bucket: "data"}}, now.Add(time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	until := now.Add(time.Minute)
	v := pausedView(s, now, until)
	if v.RetryAt == nil || v.WaitingReason != "upstream_throttled" || v.Timing.StartedAt != nil {
		t.Fatal("pause must not imply an execution start", v)
	}
	v = pausedView(s, until, until)
	if v.RetryAt != nil || v.WaitingReason != "scheduler" {
		t.Fatal("expired pause still exposed", v)
	}
}
