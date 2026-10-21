package storage

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/minio/madmin-go/v3"

	"minio-web/api/src/services/mutation"
)

func TestAdminCommandEffectsAndOneAttempt(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		effect mutation.Effect
	}{
		{"accepted", 200, "", mutation.Changed},
		{"lost response", 0, "", mutation.Uncertain},
		{"upstream error", 502, `{"Code":"InternalError"}`, mutation.Uncertain},
		{"explicit denial", 403, `{"Code":"AccessDenied"}`, mutation.Unchanged},
		{"explicit admin denial", 403, `{"Code":"XMinioAdminAccessDenied"}`, mutation.Unchanged},
		{"signature refused", 403, `{"Code":"SignatureDoesNotMatch"}`, mutation.Unchanged},
		{"opaque denial", 403, `upstream gateway failure`, mutation.Uncertain},
		{"unknown rejection", 400, `{"Code":"UnknownFailure"}`, mutation.Uncertain},
		{"untrusted error code", 502, `{"Code":"AccessDenied"}`, mutation.Uncertain},
		{"oversized success", 200, strings.Repeat("x", maxAdminResponse+1), mutation.Uncertain},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, scope := mutation.WithScope(context.Background())
			calls := 0
			failure := errors.New("test response lost")
			client := adminFixture(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if tc.status == 0 {
					return nil, failure
				}
				return adminReply(tc.status, tc.body), nil
			}))
			err := client.AddUser(ctx, "fixture-user", "fixture-secret")
			if calls != 1 || scope.Effect() != tc.effect || (tc.name == "accepted") != (err == nil) {
				t.Fatal("command replayed, effect or result changed", calls, scope.Effect(), err)
			}
			if tc.status == 0 && !errors.Is(err, failure) {
				t.Fatal("original failure lost", err)
			}
		})
	}
}

func TestAllAdminCommandsRecordUncertaintyWithoutReplay(t *testing.T) {
	for name, command := range adminCalls() {
		t.Run(name, func(t *testing.T) {
			ctx, scope := mutation.WithScope(context.Background())
			calls := 0
			client := adminFixture(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, io.ErrUnexpectedEOF
			}))
			if err := command(ctx, client); !errors.Is(err, io.ErrUnexpectedEOF) || calls != 1 || scope.Effect() != mutation.Uncertain {
				t.Fatal("mutation effect lost", calls, scope.Effect(), err)
			}
		})
	}
}

func TestAdminPreflightAndReadsDoNotRecordEffects(t *testing.T) {
	for _, kind := range []string{"SDK validation", "cancelled before call", "cancelled before transport", "read"} {
		t.Run(kind, func(t *testing.T) {
			ctx, scope := mutation.WithScope(context.Background())
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			calls := 0
			client := adminFixture(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return adminReply(200, `{"size":10}`), nil
			}))
			switch kind {
			case "SDK validation":
				_, _ = client.AddServiceAccount(ctx, madmin.AddServiceAccountReq{Name: strings.Repeat("x", 1025)})
			case "cancelled before call":
				cancel()
				_ = client.AddUser(ctx, "fixture-user", "fixture-secret")
			case "cancelled before transport":
				_ = adminChange(ctx, func(ctx context.Context) error {
					cancel()
					return client.AdminClient.AddUser(ctx, "fixture-user", "fixture-secret")
				})
			case "read":
				_, _ = client.GetBucketQuota(ctx, "fixture-bucket")
			}
			wantCalls := 0
			if kind == "read" {
				wantCalls = 1
			}
			if scope.Effect() != mutation.Unchanged || calls != wantCalls {
				t.Fatal("read or preflight failure marked a change", kind, calls, scope.Effect())
			}
		})
	}
}

func TestAdminUncertaintySurvivesSDKDecodeFailureAndPanic(t *testing.T) {
	for _, panicDuringCall := range []bool{false, true} {
		ctx, scope := mutation.WithScope(context.Background())
		calls := 0
		client := adminFixture(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			if panicDuringCall {
				panic("test failure after command handoff")
			}
			return adminReply(200, "not encrypted JSON"), nil
		}))
		func() {
			defer func() {
				if panicDuringCall && recover() == nil {
					t.Error("test panic disappeared")
				}
			}()
			_, err := client.AddServiceAccount(ctx, madmin.AddServiceAccountReq{AccessKey: "fixture-child", SecretKey: "fixture-secret"})
			if !panicDuringCall && err == nil {
				t.Error("invalid response accepted")
			}
		}()
		if calls != 1 || scope.Effect() != mutation.Uncertain {
			t.Fatal("uncertainty lost after decoding/panic", calls, scope.Effect())
		}
	}
}

func TestLaterRejectionCannotEraseCommittedEffect(t *testing.T) {
	ctx, scope := mutation.WithScope(context.Background())
	calls := 0
	client := adminFixture(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return adminReply(200, ""), nil
		}
		return adminReply(403, `{"Code":"AccessDenied"}`), nil
	}))
	if err := client.AddUser(ctx, "fixture-user", "fixture-secret"); err != nil {
		t.Fatal(err)
	}
	if err := client.SetUserStatus(ctx, "fixture-user", madmin.AccountDisabled); err == nil {
		t.Fatal("rejection ignored")
	}
	if calls != 2 || scope.Effect() != mutation.Changed {
		t.Fatal("partial commit lost", calls, scope.Effect())
	}
}
