package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
	"minio-web/api/src/services/telemetry"
)

func (app *State) AcquireMutation(ctx context.Context, s *Session, resource string) (func(), error) {
	endpoint := s.id
	if s.internal != nil {
		endpoint = s.internal.String()
	}
	started := time.Now()
	release, err := app.mutations.Acquire(ctx, endpoint+"\x00"+resource)
	telemetry.Waited(ctx, time.Since(started))
	return release, err
}
func (app *State) AcquireTransfer(principal string) (func(), error) {
	release, ok := app.transfers.Try(principal)
	if !ok {
		return nil, apperror.New(429, "TransferLimit", messages.RuntimeTransferLimit)
	}
	return release, nil
}
func (app *State) ActiveTransfers() int { return app.transfers.Active() }

func fingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func (app *State) AdmitLogin(address string) (func(), time.Duration, error) {
	key := fingerprint(address)
	if ok, wait := app.loginAttempts.Allow(key, time.Now()); !ok {
		return nil, wait, apperror.New(429, "LoginRateLimit", messages.RuntimeLoginRateLimit)
	}
	release, ok := app.logins.Try(key)
	if !ok {
		return nil, time.Second, apperror.New(429, "TooManyRequests", messages.RuntimeTooManyRequests)
	}
	return release, 0, nil
}
func (app *State) AdmitIdentity(endpoint, actor string) error {
	if ok, _ := app.loginIdentities.Allow(fingerprint(endpoint+"\x00"+actor), time.Now()); !ok {
		return apperror.New(429, "LoginRateLimit", messages.RuntimeLoginRateLimitAdmission)
	}
	return nil
}

func (app *State) MutationLocker(s *Session) func(context.Context, string) (func(), error) {
	return func(ctx context.Context, resource string) (func(), error) {
		return app.AcquireMutation(ctx, s, resource)
	}
}
