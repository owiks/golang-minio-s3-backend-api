package uploads

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/uploads/invite"
)

type inviteLookup struct {
	grant *uploadinvite.UploadInvite
	token string
}

func (s *inviteLookup) Get(token string) *uploadinvite.UploadInvite { s.token = token; return s.grant }

type inviteAdmission struct {
	owner           string
	calls, releases int
}

func (a *inviteAdmission) AcquireTransfer(owner string) (func(), error) {
	a.owner = owner
	a.calls++
	return func() { a.releases++ }, nil
}

type invitedWriter struct {
	ctx                      context.Context
	bucket, key, contentType string
	body                     []byte
	calls                    int
	failure                  error
	condition                string
}

func (w *invitedWriter) PutObject(ctx context.Context, bucket, key string, source io.Reader, size int64, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	w.ctx, w.bucket, w.key, w.contentType = ctx, bucket, key, opts.ContentType
	w.condition = opts.Header().Get("If-None-Match")
	w.calls++
	var err error
	w.body, err = io.ReadAll(source)
	if err != nil {
		return minio.UploadInfo{}, err
	}
	return minio.UploadInfo{Size: size}, w.failure
}

func TestReceiveInviteThroughReadPortPreservesBudgetAndCleanup(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "write-failure"}[failed], func(t *testing.T) {
			failure := errors.New("write rejected")
			writer := &invitedWriter{}
			if failed {
				writer.failure = failure
			}
			grant := uploadinvite.New(uploadinvite.Config{Owner: "owner", Bucket: "test-bucket", Prefix: "incoming/", Expires: time.Now().Add(time.Hour), MaxSize: 4, MaxFiles: 1, Writer: writer})
			store := &inviteLookup{grant: grant}
			admission := &inviteAdmission{}
			value, err := ReceiveFile(context.Background(), store, admission, "invite-token", "file.txt", "text/plain", 4, 4, bytes.NewBufferString("body"))
			if failed {
				if !errors.Is(err, failure) || grant.Snapshot().Uploaded != 0 || value.OK {
					t.Fatal("failed write counted", err)
				}
			} else if err != nil || !value.OK || grant.Snapshot().Uploaded != 1 {
				t.Fatal("successful write not counted", err)
			}
			if store.token != "invite-token" || writer.bucket != "test-bucket" || writer.key != "incoming/file.txt" || writer.contentType != "text/plain" || string(writer.body) != "body" || writer.calls != 1 || admission.owner != "owner" || admission.calls != 1 || admission.releases != 1 {
				t.Fatal("invited write scope/admission lost")
			}
			if writer.condition != "*" {
				t.Fatal("invitation lost create-only condition")
			}
			if deadline, ok := writer.ctx.Deadline(); !ok || !deadline.Equal(grant.Expires()) || writer.ctx.Err() != context.Canceled {
				t.Fatal("invite write context not bounded/released")
			}
			info, err := InspectUploadInvite(context.Background(), store, "invite-token")
			if err != nil || info.Uploaded != grant.Snapshot().Uploaded || info.MaxFiles != 1 || info.MaxSize != 4 {
				t.Fatal("invite snapshot lost", err)
			}
			grant.Stop()
			_, err = ReceiveFile(context.Background(), store, admission, "invite-token", "file.txt", "text/plain", 4, 4, bytes.NewBufferString("body"))
			if err == nil || apperror.Public(err).Code != "InviteExpired" || writer.calls != 1 || admission.calls != 1 {
				t.Fatal("revoked invite wrote file", err)
			}
		})
	}
	if _, err := InspectUploadInvite(context.Background(), &inviteLookup{}, "missing"); err == nil || apperror.Public(err).Status != 404 {
		t.Fatal("missing invitation exposed", err)
	}
}
