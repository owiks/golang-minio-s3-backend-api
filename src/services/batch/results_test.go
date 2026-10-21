package batch

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/minio/madmin-go/v3"
)

func TestBatchMetricIsWhitelistedAndKeepsZeroCounters(t *testing.T) {
	raw, err := json.Marshal(SafeBatchMetric(madmin.JobMetric{Catalog: &madmin.CatalogInfo{ErrorMsg: "PRIVATE-UPSTREAM", ManifestPathBucket: "PRIVATE-BUCKET"}}))
	if err != nil || strings.Contains(string(raw), "PRIVATE") || !strings.Contains(string(raw), `"objectsScannedCount":0`) || strings.Contains(string(raw), `"rotation"`) {
		t.Fatalf("batch metric shape/privacy: %s %v", raw, err)
	}
}
