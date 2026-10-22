package status

import (
	"context"
	"net/http"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
)

const readinessTimeout = time.Second

// Ready checks only the API's required state store, never user S3 connections.
// Its deadline also bounds acquiring a connection from an exhausted SQL pool.
func Ready(w http.ResponseWriter, r *http.Request, check func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
	defer cancel()
	if err := check(ctx); err != nil || ctx.Err() != nil {
		// Do not expose driver errors, connection details or credentials.
		return authstate.Unavailable()
	}
	Health(w, r)
	return nil
}
