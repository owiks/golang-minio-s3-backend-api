package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"minio-web/api/src/config"
	"minio-web/api/src/http/api/common"
	"minio-web/api/src/http/api/routes"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
	"minio-web/api/src/services/runtime"
	"minio-web/api/src/services/storage"
	"minio-web/api/tests/testsession"
)

func TestAccessRevisionSurvivesPartialFailureCancellationAndPanic(t *testing.T) {
	for _, scenario := range []string{"partial commit", "mapped uncertainty", "cancel during command", "panic after commit", "panic during command", "non IAM"} {
		t.Run(scenario, func(t *testing.T) {
			app := runtime.New(config.Config{})
			defer app.Close()
			s := testsession.New(t, "http://fixture.invalid:9000")
			if err := app.StoreSession(s); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			endpoint, _ := url.Parse("http://fixture.invalid:9000")
			client, err := storage.NewAdmin(storage.Config{Endpoint: endpoint, Credentials: credentials.NewStaticV4("fixture-user", "fixture-secret", "")}, upstreamTestWire(func(r *http.Request) (*http.Response, error) {
				calls++
				if scenario == "panic during command" {
					panic("test panic after handoff")
				}
				if scenario == "cancel during command" {
					cancel()
					return nil, r.Context().Err()
				}
				if scenario == "mapped uncertainty" {
					return nil, io.ErrUnexpectedEOF
				}
				if calls == 2 {
					return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader(`{"Code":"AccessDenied"}`)), Header: make(http.Header)}, nil
				}
				return &http.Response{StatusCode: 200, Body: http.NoBody, Header: make(http.Header)}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			match := routes.Match{Found: true, MethodAllowed: true, Params: map[string]string{"sessionID": s.SessionID()}, Operation: routes.Operation{Method: "POST", Mutation: routes.IAMLock, InvalidatesAccess: scenario != "non IAM", Handler: func(_ *runtime.State, _ http.ResponseWriter, r *http.Request, _ *runtime.Session, _ string) error {
				if err := client.AddUser(r.Context(), "fixture-user", "fixture-secret"); err != nil {
					return apperror.NewWithCause(502, "SafeMappedError", messages.Literal("safe"), err)
				}
				if scenario == "panic after commit" {
					panic("test failure after commit")
				}
				if scenario == "partial commit" {
					return client.SetUserStatus(r.Context(), "fixture-user", madmin.AccountDisabled)
				}
				return apperror.New(409, "LaterDomainFailure", messages.Literal("safe"))
			}}}
			previous := app.AccessRevision(s)
			r := httptest.NewRequest("POST", "http://ui.example/api/sessions/"+s.SessionID()+"/admin/users", nil).WithContext(ctx)
			r.Header.Set("Origin", "http://ui.example")
			r.Header.Set("X-CSRF-Token", s.CSRFToken())
			r.AddCookie(common.SessionCookie(s, r, false))
			panicked := false
			func() {
				defer func() { panicked = recover() != nil }()
				err = executeOperation(app, httptest.NewRecorder(), r, match)
			}()
			if panicked != strings.HasPrefix(scenario, "panic") || !panicked && err == nil {
				t.Fatal("handler outcome changed", scenario, panicked, err)
			}
			wantCalls := 1
			if scenario == "partial commit" {
				wantCalls = 2
			}
			split := strings.LastIndex(previous, ":")
			sequence, parseErr := strconv.ParseUint(previous[split+1:], 10, 64)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			wantRevision := previous[:split+1] + strconv.FormatUint(sequence+1, 10)
			if scenario == "non IAM" {
				wantRevision = previous
			}
			if app.AccessRevision(s) != wantRevision || calls != wantCalls {
				t.Fatal("revision not advanced exactly once or command replayed", scenario, previous, app.AccessRevision(s), calls)
			}
		})
	}
}
