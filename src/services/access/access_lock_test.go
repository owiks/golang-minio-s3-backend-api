package access

import (
	"context"
	"errors"
	"testing"
	"time"
)

type discoveryLockClient struct {
	DiscoverAccessClient // Any probe call in these cases must fail the test.
	lock                 func(context.Context) (func(), error)
}

func (c discoveryLockClient) LockAccess(ctx context.Context) (func(), error) {
	return c.lock(ctx)
}

func TestDiscoverAccessIncludesLockWaitInBudgetAndCleansFailedAcquisition(t *testing.T) {
	for _, earlier := range []bool{false, true} {
		t.Run(map[bool]string{false: "discovery deadline", true: "caller deadline"}[earlier], func(t *testing.T) {
			parent, cancel := context.WithTimeout(context.Background(), time.Hour)
			if earlier {
				cancel()
				parent, cancel = context.WithTimeout(context.Background(), time.Second)
			}
			defer cancel()
			failure := errors.New("lock failed")
			var received context.Context
			client := discoveryLockClient{lock: func(ctx context.Context) (func(), error) {
				received = ctx
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 8*time.Second {
					t.Fatal("lock wait is outside the discovery budget", deadline)
				}
				if earlier {
					want, _ := parent.Deadline()
					if deadline != want {
						t.Fatal("caller deadline replaced by a longer one")
					}
				}
				return nil, failure
			}}
			buckets, access, err := DiscoverAccess(parent, client)
			if !errors.Is(err, failure) || buckets != nil || access.Sections != nil {
				t.Fatal("lock failure hidden by an empty successful result", err)
			}
			if received == nil || received.Err() != context.Canceled || parent.Err() != nil {
				t.Fatal("failed acquisition leaked its deadline or cancelled caller")
			}
		})
	}
}

func TestDiscoverAccessCancellationAfterAcquisitionReleasesWithoutProbing(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	released := 0
	client := discoveryLockClient{lock: func(context.Context) (func(), error) {
		cancel()
		return func() { released++ }, nil
	}}
	_, _, err := DiscoverAccess(parent, client)
	if !errors.Is(err, context.Canceled) || released != 1 {
		t.Fatal("cancelled discovery probed MinIO or retained the lock", released, err)
	}
}
