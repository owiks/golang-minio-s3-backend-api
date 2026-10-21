package capabilities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
	"minio-web/api/src/services/storage"
)

type probeFixture struct {
	attributes, list int
	err              error
	cancel           context.CancelFunc
	configured       bool
}

func (f *probeFixture) ConditionalCompletionConfigured() bool { return f.configured }

func (f *probeFixture) ReadObjectAttributes(ctx context.Context, bucket, key string, options storage.ObjectAttributesOptions) (storage.ObjectAttributesPage, error) {
	f.attributes++
	if options.MaxParts != 1 || options.PartNumberMarker != 0 {
		panic("unbounded attributes probe")
	}
	if f.cancel != nil {
		f.cancel()
	}
	return storage.ObjectAttributesPage{}, f.err
}
func (f *probeFixture) ListMultipartUploads(ctx context.Context, bucket, prefix, keyMarker, uploadIDMarker, delimiter string, limit int) (minio.ListMultipartUploadsResult, error) {
	f.list++
	if limit != 1 || prefix != "" || keyMarker != "" || uploadIDMarker != "" || delimiter != "" {
		panic("unbounded multipart probe")
	}
	return minio.ListMultipartUploadsResult{}, f.err
}

func TestOutcomeClassifiesOnlyDefiniteProtocolEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status Status
		reason string
	}{
		{"success", nil, Supported, "OperationSucceeded"},
		{"denied", minio.ErrorResponse{Code: "AccessDenied", Message: "SECRET"}, AccessDenied, "AccessDenied"},
		{"pointer", &minio.ErrorResponse{Code: "AccessDenied"}, AccessDenied, "AccessDenied"},
		{"unsupported", fmt.Errorf("wrapped: %w", minio.ErrorResponse{Code: "NotImplemented"}), Unsupported, "OperationNotImplemented"},
		{"missing", minio.ErrorResponse{Code: "NoSuchKey"}, Unknown, "ObjectNotFound"},
		{"bucket", minio.ErrorResponse{Code: "NoSuchBucket"}, Unknown, "BucketNotFound"},
		{"timeout", context.DeadlineExceeded, Unknown, "Timeout"},
		{"network", errors.New("SECRET network failure"), Unknown, "UpstreamUncertain"},
		{"5xx", minio.ErrorResponse{Code: "InternalError", StatusCode: 500}, Unknown, "UpstreamUncertain"},
		{"ambiguous5xx", minio.ErrorResponse{Code: "UnsupportedOperation", StatusCode: 503}, Unknown, "UpstreamUncertain"},
		{"malformed", apperror.New(502, "InvalidObjectAttributesResponse", messages.Literal("SECRET")), Unknown, "InvalidResponse"},
		{"known unsupported", apperror.New(501, "ObjectAttributesUnsupported", messages.Literal("SECRET")), Unsupported, "OperationNotImplemented"},
		{"validated protocol unsupported", apperror.New(501, "NotImplemented", messages.Literal("SECRET")), Unsupported, "OperationNotImplemented"},
		{"safe denied", apperror.New(403, "AccessDenied", messages.Literal("SECRET")), AccessDenied, "AccessDenied"},
		{"generic501", apperror.New(501, "UpstreamError", messages.Literal("SECRET")), Unknown, "UpstreamUncertain"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
			value := Outcome(ObjectAttributes, Scope{Bucket: "test-bucket", Key: "file.csv"}, "read_probe", tc.err, now)
			if value.Status != tc.status || value.Reason != tc.reason || !value.ExpiresAt.Equal(now.Add(TTL(tc.status))) {
				t.Fatal(value)
			}
			wantAccess := AccessUnknown
			if tc.status == Supported {
				wantAccess = AccessAllowed
			} else if tc.status == AccessDenied {
				wantAccess = AccessForbidden
			}
			if value.Access != wantAccess {
				t.Fatal("support confused with access", value)
			}
			data, _ := json.Marshal(value)
			if strings.Contains(string(data), "SECRET") {
				t.Fatal("upstream body leaked", string(data))
			}
		})
	}
}

func TestReportOnlyRunsBoundedReadOperations(t *testing.T) {
	f := &probeFixture{}
	value, err := Report(context.Background(), f, nil, Request{Scope: Scope{Bucket: "test-bucket", Key: "test.csv", VersionID: "version"}})
	if err != nil || len(value.Capabilities) != len(Identifiers()) || f.attributes != 1 || f.list != 1 {
		t.Fatal(value, err, f)
	}
	for _, result := range value.Capabilities {
		if result.ID == ObjectAttributes || result.ID == MultipartList {
			if result.Status != Supported {
				t.Fatal(result)
			}
		} else if result.Status != Unknown || result.Reason != "RequiresObservedOperation" {
			t.Fatal("write or Select probed", result)
		}
	}
	// No resource scope means no network, rather than probing every bucket or
	// arbitrary object during login.
	f = &probeFixture{}
	value, err = Report(context.Background(), f, nil, Request{})
	if err != nil || f.attributes+f.list != 0 {
		t.Fatal(value, err, f)
	}
	for _, result := range value.Capabilities {
		if result.Status != Unknown {
			t.Fatal(result)
		}
	}
}

func TestReportScopeValidationAndCancellation(t *testing.T) {
	for _, request := range []Request{
		{Feature: "invented"}, {Scope: Scope{Key: "key"}}, {Scope: Scope{Bucket: "test-bucket", VersionID: "v"}},
		{Scope: Scope{Bucket: "INVALID-BUCKET"}}, {Scope: Scope{Bucket: "test-bucket", Key: strings.Repeat("x", 1025)}},
		{Scope: Scope{Bucket: "test-bucket", Key: "key", VersionID: "bad\x00"}},
	} {
		f := &probeFixture{}
		_, err := Report(context.Background(), f, nil, request)
		if err == nil || f.attributes+f.list != 0 {
			t.Fatal(request, err, f)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	f := &probeFixture{cancel: cancel}
	_, err := Report(ctx, f, nil, Request{Feature: ObjectAttributes, Scope: Scope{Bucket: "test-bucket", Key: "key"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("canceled request became capability", err)
	}
}

func TestCatalogIsIndependentAndChecksumModesDistinct(t *testing.T) {
	ids := Identifiers()
	first := ids[0]
	ids[0] = "mutated"
	if Identifiers()[0] != first || Known("mutated") {
		t.Fatal("mutable catalog")
	}
	if ChecksumID("SHA256", "FULL_OBJECT") == ChecksumID("SHA256", "COMPOSITE") || ChecksumID("invalid", "FULL_OBJECT") != "" || ChecksumID("MD5", "FULL_OBJECT") == "" {
		t.Fatal("checksum modes collapsed")
	}
}

func TestConditionalCompleteConfigurationIsExplicitEvidenceWithoutProbe(t *testing.T) {
	for _, configured := range []bool{false, true} {
		f := &probeFixture{configured: configured}
		value, err := Report(context.Background(), f, nil, Request{Feature: ConditionalComplete})
		if err != nil || f.attributes+f.list != 0 || len(value.Capabilities) != 1 {
			t.Fatal(value, err, f)
		}
		r := value.Capabilities[0]
		if configured {
			if r.Status != Supported || r.Access != AccessUnknown || r.Evidence != "confirmed_configuration" || r.Reason != "EndpointConfigured" {
				t.Fatal(r)
			}
		} else if r.Status != Unknown {
			t.Fatal("missing configuration claimed unsupported/supported", r)
		}
	}
}
