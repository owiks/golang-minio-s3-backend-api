package storage

import (
	"context"
	"net/http"
	"time"

	"github.com/minio/madmin-go/v3"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

// Open only the authenticated read-stream handshake, then cancel immediately.
// Do not collect, return or persist logs/traces during navigation discovery.
type accessStreamTransport struct {
	Base   http.RoundTripper `json:"-"`
	Result chan error        `json:"-"`
}

func (t accessStreamTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := t.Base.RoundTrip(r)
	result := err
	if err == nil && resp.StatusCode != http.StatusOK {
		result = apperror.New(resp.StatusCode, "AccessDenied", messages.StorageAccessDeniedDiagnosticProbe)
		if resp.StatusCode != 403 {
			result = apperror.New(resp.StatusCode, "ProbeUnavailable", messages.StorageProbeUnavailable)
		}
	}
	select {
	case t.Result <- result:
	default:
	}
	return resp, err
}
func ProbeDiagnostic(ctx context.Context, config Config, transport http.RoundTripper, trace bool) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	result := make(chan error, 1)
	admin, err := NewAdmin(config, accessStreamTransport{Base: transport, Result: result})
	if err != nil {
		return err
	}
	if trace {
		ch := admin.ServiceTrace(ctx, madmin.ServiceTraceOpts{S3: true})
		go func() {
			for range ch {
			}
		}()
	} else {
		ch := admin.GetLogs(ctx, "", 1, "ALL")
		go func() {
			for range ch {
			}
		}()
	}
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
