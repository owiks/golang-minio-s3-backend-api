package requestctx

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDeadlineUsesMetadataAndParentCancellation(t *testing.T) {
	for _, long := range []bool{false, true} {
		parent, stop := context.WithCancel(context.Background())
		r, cancel, budget := WithDeadline(httptest.NewRequest("GET", "/arbitrary", nil).WithContext(parent), long)
		want := 30 * time.Second
		if long {
			want = 30 * time.Minute
		}
		deadline, ok := r.Context().Deadline()
		if !ok || budget != want || time.Until(deadline) > want {
			t.Fatal("wrong operation budget")
		}
		stop()
		if r.Context().Err() != context.Canceled {
			t.Fatal("lost parent cancellation")
		}
		cancel()
	}
}
