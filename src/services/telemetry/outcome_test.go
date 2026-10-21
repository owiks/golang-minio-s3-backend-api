package telemetry

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestDomainOutcomesAndCounters(t *testing.T) {
	for _, result := range []string{"success", "partial", "cancelled", "error"} {
		t.Run(result, func(t *testing.T) {
			ctx, trace := Start(context.Background(), "POST static-route")
			Outcome(ctx, result, "SafeCode")
			if got := trace.ResultName(ctx, 200); got != result {
				t.Fatal(got)
			}
			m := Metrics{}
			m.Begin()
			m.End(trace.Operation, 200, time.Second, trace.ResultName(ctx, 200))
			c := m.operations[trace.Operation]
			if (c.Errors == 1) != (result == "partial" || result == "error") || (c.Partial == 1) != (result == "partial") || (c.Cancelled == 1) != (result == "cancelled") {
				t.Fatal(c)
			}
			var logs bytes.Buffer
			old := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
			defer slog.SetDefault(old)
			trace.Log(ctx, 200, 10, time.Second)
			if !strings.Contains(logs.String(), `"result":"`+result+`"`) {
				t.Fatal(logs.String())
			}
		})
	}
}
