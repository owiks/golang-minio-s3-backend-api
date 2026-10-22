package authstate_test

import (
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/tests/statecontract"
)

func TestMemoryContract(t *testing.T) {
	repo := authstate.NewMemory()
	defer repo.Close()
	statecontract.Run(t, repo)
}
