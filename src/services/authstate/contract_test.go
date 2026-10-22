package authstate_test

import (
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/tests/statecontract"
)

func TestMemoryContract(t *testing.T) {
	repo := authstate.NewMemory()
	defer repo.Close()
	statecontract.Run(t, repo)
}

func TestMemoryClockContract(t *testing.T) {
	statecontract.RunClock(t, func(_ *testing.T, now func() time.Time) authstate.Repository {
		return authstate.NewMemoryWithClock(now)
	})
}
