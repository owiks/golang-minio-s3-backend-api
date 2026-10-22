package api

import (
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/operationpolicy"
)

func checkOperationPolicy(policy operationpolicy.Policy, match routes.Match) error {
	return policy.Check(routes.OperatorAction(match.Route, match.Operation))
}
