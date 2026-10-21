package storage

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/minio/madmin-go/v3"

	"minio-web/api/src/services/mutation"
	"minio-web/api/src/services/telemetry"
)

// AdminClient preserves SDK signing, encryption and read retries. Commands use
// an independent attempt budget; the SDK's process-wide MaxRetry is untouched.
type AdminClient struct{ *madmin.AdminClient }

type adminCommandKey struct{}
type adminAttempt struct {
	mu       sync.Mutex
	sent     bool
	err      error
	rejected bool
}

func adminCommand[T any](ctx context.Context, call func(context.Context) (T, error)) (value T, err error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	attempt := &adminAttempt{}
	completed := false
	defer func() {
		attempt.mu.Lock()
		defer attempt.mu.Unlock()
		if !attempt.sent || attempt.rejected {
			return
		}
		effect := mutation.Uncertain
		if completed && attempt.err == nil && err == nil {
			effect = mutation.Changed
		}
		mutation.Record(ctx, effect)
	}()
	value, err = call(context.WithValue(ctx, adminCommandKey{}, attempt))
	completed = true
	attempt.mu.Lock()
	defer attempt.mu.Unlock()
	if attempt.err != nil {
		telemetry.SDKError(ctx, attempt.err)
		return zero, attempt.err
	}
	telemetry.SDKError(ctx, err)
	return value, err
}

func adminChange(ctx context.Context, call func(context.Context) error) error {
	_, err := adminCommand(ctx, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, call(ctx)
	})
	return err
}

type adminTransport struct{ base http.RoundTripper }

func (t adminTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	stop := func() (*http.Response, error) {
		if r.Body != nil {
			r.Body.Close()
		}
		return stoppedAdminResponse(), nil
	}
	attempt, _ := r.Context().Value(adminCommandKey{}).(*adminAttempt)
	if attempt == nil {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			return t.base.RoundTrip(r)
		}
		// An unadapted SDK mutation must never silently regain its retry policy.
		return stop()
	}
	attempt.mu.Lock()
	defer attempt.mu.Unlock()
	if attempt.sent {
		if attempt.err == nil {
			attempt.err = errors.New("admin command attempted more than one request")
		}
		return stop()
	}
	if err := r.Context().Err(); err != nil {
		attempt.err = err
		return stop()
	}
	attempt.sent = true
	response, err := t.base.RoundTrip(r)
	if err != nil {
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		attempt.err = err
	} else if response == nil || response.Body == nil {
		attempt.err = errors.New("empty admin response")
	} else if failure, ok := response.Body.(adminFailedBody); ok {
		attempt.err = failure.err
	} else if response.StatusCode >= 200 && response.StatusCode < 300 {
		return response, nil
	} else {
		attempt.err = adminResponseError(response)
		attempt.rejected = adminAuthorizationRejected(response.StatusCode, attempt.err)
	}
	// madmin retries every transport error and selected HTTP/error codes, with
	// no per-client switch. A private terminal response ends that loop without
	// cancelling the caller. adminCommand restores the ORIGINAL failure; this
	// response never leaves the adapter. Redirects cannot replay a command either.
	return stoppedAdminResponse(), nil
}

// Only explicit native authentication/authorization refusals establish that a
// command was rejected. Unknown 4xx, proxy/5xx and malformed responses remain
// uncertain; status alone cannot prove the absence of a partial change.
func adminAuthorizationRejected(status int, err error) bool {
	if status != http.StatusUnauthorized && status != http.StatusForbidden {
		return false
	}
	var native madmin.ErrorResponse
	if !errors.As(err, &native) {
		return false
	}
	switch native.Code {
	case "AccessDenied", "XMinioAdminAccessDenied", "InvalidAccessKeyId", "SignatureDoesNotMatch", "ExpiredToken", "InvalidToken":
		return true
	}
	return false
}

func stoppedAdminResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusBadRequest, Status: "400 Bad Request",
		Header: http.Header{"Content-Type": {"application/json"}},
		Body:   io.NopCloser(strings.NewReader(`{"Code":"AdminCommandStopped","Message":"Admin command stopped."}`)),
	}
}

// Match the SDK's JSON/XML error representation and 100 KiB parsing bound.
// Invalid or oversized bodies remain opaque upstream errors, never raw text.
func adminResponseError(response *http.Response) error {
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, (100<<10)+1))
	if err != nil {
		return err
	}
	if len(body) <= 100<<10 {
		var result madmin.ErrorResponse
		if json.Unmarshal(body, &result) == nil || xml.Unmarshal(body, &result) == nil {
			return result
		}
	}
	return errors.New("invalid admin error response")
}
