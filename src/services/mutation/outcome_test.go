package mutation

import (
	"context"
	"sync"
	"testing"
)

func TestScopeRetainsPartialAndUncertainEffects(t *testing.T) {
	ctx, scope := WithScope(context.Background())
	other, isolated := WithScope(context.Background())
	var work sync.WaitGroup
	for range 100 {
		work.Go(func() { Record(ctx, Changed); Record(ctx, Unchanged) })
		work.Go(func() { Record(other, Unchanged) })
	}
	work.Wait()
	if scope.Effect() != Changed || isolated.Effect() != Unchanged {
		t.Fatal("partial effect lost or another request contaminated")
	}
	Record(ctx, Uncertain)
	Record(ctx, Changed)
	Record(ctx, Effect(999))
	if scope.Effect() != Uncertain || isolated.Effect() != Unchanged {
		t.Fatal("uncertain effect overwritten")
	}
	Record(context.Background(), Changed) // Observation is optional for service callers.
}
