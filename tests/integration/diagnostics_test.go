package integration

import (
	"encoding/json"
	"testing"

	"github.com/minio/madmin-go/v3"

	serversvc "github.com/owiks/golang-minio-s3-backend-api/src/services/server"
)

func TestDiagnosticZeroAndAbsent(t *testing.T) {
	for _, tc := range []struct {
		Name     string                  `json:"-"`
		Row      serversvc.DiagnosticRow `json:"-"`
		Http     bool                    `json:"-"`
		Duration bool                    `json:"-"`
	}{
		{"http zeros", serversvc.TraceRow(madmin.TraceInfo{HTTP: &madmin.TraceHTTPStats{}}), true, true},
		{"no http", serversvc.TraceRow(madmin.TraceInfo{}), false, true},
		{"log", serversvc.DiagnosticRow{Level: "ERROR"}, false, false},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			raw, err := json.Marshal(tc.Row)
			if err != nil {
				t.Fatal(err)
			}
			var result map[string]any
			if err = json.Unmarshal(raw, &result); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"input", "output"} {
				value, ok := result[key]
				if ok != tc.Http {
					t.Fatalf("%s presence: %s", key, raw)
				}
				if ok && value != float64(0) {
					t.Fatal(key, value)
				}
			}
			value, ok := result["durationMs"]
			if ok != tc.Duration {
				t.Fatalf("duration presence: %s", raw)
			}
			if ok && value != float64(0) {
				t.Fatal(value)
			}
		})
	}
}
