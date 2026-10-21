package storage

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/telemetry"
)

func adminFixture(t *testing.T, transport http.RoundTripper) *AdminClient {
	t.Helper()
	endpoint, _ := url.Parse("http://fixture.invalid:9000")
	client, err := NewAdmin(Config{Endpoint: endpoint, Credentials: credentials.NewStaticV4("fixture-access", "fixture-secret", "fixture-token")}, transport)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func adminReply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func adminCalls() map[string]func(context.Context, *AdminClient) error {
	return map[string]func(context.Context, *AdminClient) error{
		"AddUser": func(ctx context.Context, c *AdminClient) error {
			return c.AddUser(ctx, "fixture-user", "fixture-secret")
		},
		"SetUserStatus": func(ctx context.Context, c *AdminClient) error {
			return c.SetUserStatus(ctx, "fixture-user", madmin.AccountDisabled)
		},
		"RemoveUser": func(ctx context.Context, c *AdminClient) error { return c.RemoveUser(ctx, "fixture-user") },
		"UpdateGroupMembers": func(ctx context.Context, c *AdminClient) error {
			return c.UpdateGroupMembers(ctx, madmin.GroupAddRemove{Group: "fixture-group", Members: []string{"fixture-user"}})
		},
		"SetGroupStatus": func(ctx context.Context, c *AdminClient) error {
			return c.SetGroupStatus(ctx, "fixture-group", madmin.GroupDisabled)
		},
		"AddCannedPolicy": func(ctx context.Context, c *AdminClient) error {
			return c.AddCannedPolicy(ctx, "fixture-policy", []byte(`{"Version":"2012-10-17","Statement":[]}`))
		},
		"RemoveCannedPolicy": func(ctx context.Context, c *AdminClient) error { return c.RemoveCannedPolicy(ctx, "fixture-policy") },
		"AttachPolicy": func(ctx context.Context, c *AdminClient) error {
			_, err := c.AttachPolicy(ctx, madmin.PolicyAssociationReq{User: "fixture-user", Policies: []string{"fixture-policy"}})
			return err
		},
		"DetachPolicy": func(ctx context.Context, c *AdminClient) error {
			_, err := c.DetachPolicy(ctx, madmin.PolicyAssociationReq{User: "fixture-user", Policies: []string{"fixture-policy"}})
			return err
		},
		"AddServiceAccount": func(ctx context.Context, c *AdminClient) error {
			_, err := c.AddServiceAccount(ctx, madmin.AddServiceAccountReq{AccessKey: "fixture-child", SecretKey: "fixture-secret"})
			return err
		},
		"UpdateServiceAccount": func(ctx context.Context, c *AdminClient) error {
			return c.UpdateServiceAccount(ctx, "fixture-child", madmin.UpdateServiceAccountReq{NewStatus: "off"})
		},
		"DeleteServiceAccount": func(ctx context.Context, c *AdminClient) error { return c.DeleteServiceAccount(ctx, "fixture-child") },
		"SetBucketQuota": func(ctx context.Context, c *AdminClient) error {
			return c.SetBucketQuota(ctx, "fixture-bucket", &madmin.BucketQuota{Size: 10, Type: madmin.HardQuota})
		},
		"SetConfigKV": func(ctx context.Context, c *AdminClient) error {
			_, err := c.SetConfigKV(ctx, "scanner speed=slow")
			return err
		},
		"AddTier": func(ctx context.Context, c *AdminClient) error {
			tier, err := madmin.NewTierS3("ARCHIVE", "fixture-access", "fixture-secret", "archive", madmin.S3Endpoint("https://fixture.invalid"))
			if err != nil {
				return err
			}
			return c.AddTier(ctx, tier)
		},
		"EditTier":   func(ctx context.Context, c *AdminClient) error { return c.EditTier(ctx, "ARCHIVE", madmin.TierCreds{}) },
		"RemoveTier": func(ctx context.Context, c *AdminClient) error { return c.RemoveTier(ctx, "ARCHIVE") },
		"VerifyTier": func(ctx context.Context, c *AdminClient) error { return c.VerifyTier(ctx, "ARCHIVE") },
		"StartBatchJob": func(ctx context.Context, c *AdminClient) error {
			_, err := c.StartBatchJob(ctx, "fixture-job")
			return err
		},
		"CancelBatchJob":         func(ctx context.Context, c *AdminClient) error { return c.CancelBatchJob(ctx, "fixture-job") },
		"RebalanceStart":         func(ctx context.Context, c *AdminClient) error { _, err := c.RebalanceStart(ctx); return err },
		"RebalanceStop":          func(ctx context.Context, c *AdminClient) error { return c.RebalanceStop(ctx) },
		"DecommissionPool":       func(ctx context.Context, c *AdminClient) error { return c.DecommissionPool(ctx, "/pool2") },
		"CancelDecommissionPool": func(ctx context.Context, c *AdminClient) error { return c.CancelDecommissionPool(ctx, "/pool2") },
		"ExecuteMethod": func(ctx context.Context, c *AdminClient) error {
			response, err := c.ExecuteMethod(ctx, http.MethodPost, madmin.RequestData{RelPath: "/v3/fixture"})
			if response != nil {
				_ = response.Body.Close()
			}
			return err
		},
	}
}

func TestAllAdminCommandsHaveOneAttemptAndPreserveFailure(t *testing.T) {
	lostReply := errors.New("fixture lost reply after accepting command")
	for name, call := range adminCalls() {
		for _, networkFailure := range []bool{false, true} {
			suffix := "/http-502"
			if networkFailure {
				suffix = "/lost-reply"
			}
			t.Run(name+suffix, func(t *testing.T) {
				calls := 0
				ctx, trace := telemetry.Start(context.Background(), "PUT fixture")
				ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
				defer cancel()
				client := adminFixture(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					if r.URL.Host != "fixture.invalid:9000" || r.Header.Get("Authorization") == "" || r.Header.Get("X-Amz-Security-Token") != "fixture-token" {
						t.Error("signing or endpoint changed")
					}
					if networkFailure {
						return nil, lostReply
					}
					return adminReply(502, `{"Code":"RequestTimeout","Message":"fixture","RequestID":"original-id"}`), nil
				}))
				err := call(ctx, client)
				if calls != 1 || trace.UpstreamCalls != 1 || err == nil {
					t.Fatalf("attempts=%d upstream=%d error=%v", calls, trace.UpstreamCalls, err)
				}
				if networkFailure {
					if !errors.Is(err, lostReply) {
						t.Fatal("original network error lost")
					}
				} else if issue := madmin.ToErrorResponse(err); issue.Code != "RequestTimeout" || issue.RequestID != "original-id" {
					t.Fatal("original server error lost", issue)
				}
				if ctx.Err() != nil {
					t.Fatal("adapter cancelled caller or waited for SDK retries")
				}
			})
		}
	}
}

func TestAdminStatusCodeAndErrorCodeCannotRetryOrRedirect(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308, 400, 403, 408, 429, 500, 502, 503, 504} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			client := adminFixture(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				reply := adminReply(status, `<Error><Code>SlowDown</Code><Message>fixture</Message><RequestId>original</RequestId></Error>`)
				reply.Header.Set("Location", "http://other.invalid/minio/admin/v3/start-job")
				return reply, nil
			}))
			_, err := client.StartBatchJob(context.Background(), "fixture")
			if calls != 1 || madmin.ToErrorResponse(err).Code != "SlowDown" {
				t.Fatal("retry, redirect or changed error", calls, err)
			}
		})
	}
}

func TestAdminSuccessAndIndependentCommandsOnSameContext(t *testing.T) {
	calls := 0
	client := adminFixture(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return adminReply(200, `{"id":"original-job","type":"expire"}`), nil
	}))
	ctx := context.Background()
	for range 2 {
		job, err := client.StartBatchJob(ctx, "fixture")
		if err != nil || job.ID != "original-job" {
			t.Fatal(job, err)
		}
	}
	if calls != 2 {
		t.Fatal("independent commands share an attempt budget", calls)
	}
}

func TestAdminCancellationDoesNotBecomeInternalStopError(t *testing.T) {
	for _, cancelledBefore := range []bool{true, false} {
		t.Run(map[bool]string{true: "before", false: "during"}[cancelledBefore], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cancelledBefore {
				cancel()
			}
			calls := 0
			client := adminFixture(t, roundTripFunc(func(r *http.Request) (*http.Response, error) { calls++; cancel(); return nil, r.Context().Err() }))
			_, err := client.StartBatchJob(ctx, "fixture")
			wantCalls := 1
			if cancelledBefore {
				wantCalls = 0
			}
			if !errors.Is(err, context.Canceled) || calls != wantCalls || apperror.Public(err).Code != "Timeout" {
				t.Fatal("cancellation changed", calls, err)
			}
		})
	}
}

func TestAdminReadRetryAndGlobalPolicyRemainUnchanged(t *testing.T) {
	before := madmin.MaxRetry
	for _, networkFailure := range []bool{false, true} {
		calls := 0
		client := adminFixture(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				if networkFailure {
					return nil, io.ErrUnexpectedEOF
				}
				return adminReply(502, `{"Code":"RequestTimeout"}`), nil
			}
			return adminReply(200, `{"size":10}`), nil
		}))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		quota, err := client.GetBucketQuota(ctx, "fixture-bucket")
		cancel()
		if err != nil || quota.Size != 10 || calls != 2 {
			t.Fatal("read retry changed", quota, calls, err)
		}
	}
	if madmin.MaxRetry != before {
		t.Fatal("process-wide SDK policy modified")
	}
}

func TestConcurrentAdminCommandsDoNotShareAttemptState(t *testing.T) {
	var calls atomic.Int32
	client := adminFixture(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return adminReply(502, `{"Code":"AccessDenied","Message":"fixture"}`), nil
	}))
	ctx := context.Background()
	var wg sync.WaitGroup
	for range 24 {
		wg.Go(func() {
			_, err := client.StartBatchJob(ctx, "fixture")
			if apperror.Public(err).Code != "AccessDenied" {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if calls.Load() != 24 {
		t.Fatal("commands shared state or repeated", calls.Load())
	}
}

func TestUnadaptedAdminMutationIsRejectedBeforeNetwork(t *testing.T) {
	client := adminFixture(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("unscoped mutation reached network")
		return adminReply(204, ""), nil
	}))
	if client.AdminClient.RebalanceStop(context.Background()) == nil {
		t.Fatal("raw SDK bypass succeeded")
	}
}

type adminErrorBody struct {
	io.Reader
	closes int
}

func (b *adminErrorBody) Close() error { b.closes++; return nil }

type adminReadFailure struct{ err error }

func (b adminReadFailure) Read([]byte) (int, error) { return 0, b.err }

func TestAdminResponseFailuresStayBoundedAndCloseBodies(t *testing.T) {
	readErr := errors.New("fixture response interrupted")
	for _, tc := range []struct {
		name   string
		status int
		body   io.Reader
		want   error
	}{
		{"broken-json", 502, strings.NewReader("invalid PRIVATE-ERROR-CONTENT"), nil},
		{"oversized", 502, strings.NewReader(strings.Repeat("x", (100<<10)+1)), nil},
		{"error-body-read-failure", 502, adminReadFailure{readErr}, readErr},
		{"success-body-read-failure", 200, adminReadFailure{readErr}, readErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &adminErrorBody{Reader: tc.body}
			calls := 0
			client := adminFixture(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: body}, nil
			}))
			_, err := client.StartBatchJob(context.Background(), "fixture")
			if err == nil || calls != 1 || body.closes != 1 {
				t.Fatal("failure retried or body not closed", err, calls, body.closes)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatal("body error lost", err)
			}
			if strings.Contains(err.Error(), "PRIVATE-ERROR-CONTENT") || strings.Contains(err.Error(), "AdminCommandStopped") {
				t.Fatal("raw body or internal marker escaped")
			}
		})
	}
}

func TestFailedAdminCommandDoesNotPoisonNextCommand(t *testing.T) {
	lostReply := errors.New("fixture lost response")
	calls := 0
	client := adminFixture(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return nil, lostReply
		}
		return adminReply(200, `{"id":"second-job"}`), nil
	}))
	ctx := context.Background()
	if _, err := client.StartBatchJob(ctx, "first"); !errors.Is(err, lostReply) {
		t.Fatal(err)
	}
	job, err := client.StartBatchJob(ctx, "second")
	if err != nil || calls != 2 || job.ID != "second-job" {
		t.Fatal("failed command poisoned context", calls, job, err)
	}
}

func TestAdminSDKValidationStillPrecedesSending(t *testing.T) {
	client := adminFixture(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("invalid command reached transport")
		return adminReply(200, "{}"), nil
	}))
	if _, err := client.AddServiceAccount(context.Background(), madmin.AddServiceAccountReq{Name: strings.Repeat("x", 1025)}); err == nil {
		t.Fatal("SDK validation bypassed")
	}
}
