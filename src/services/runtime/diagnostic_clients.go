package runtime

import (
	"context"
	"github.com/minio/madmin-go/v3"
	"minio-web/api/src/services/storage"
)

func (c *Clients) OpenDiagnosticTrace(parent context.Context, onlyErrors bool) (storage.DiagnosticStream[madmin.ServiceTraceInfo], error) {
	return storage.OpenDiagnosticTrace(parent, c.session.clientConfig(), c.session.transport, onlyErrors)
}
func (c *Clients) OpenDiagnosticLogs(parent context.Context, limit int) (storage.DiagnosticStream[madmin.LogInfo], error) {
	return storage.OpenDiagnosticLogs(parent, c.session.clientConfig(), c.session.transport, limit)
}
