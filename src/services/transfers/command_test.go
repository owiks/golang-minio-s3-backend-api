package transfers_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/tags"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/requestctx"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/commandlog"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/localization"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	objectstate "github.com/owiks/golang-minio-s3-backend-api/src/services/objects/state"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/transfers"
)

type commandClient struct {
	transfers.TransferClient
	source       minio.ObjectInfo
	destination  *minio.ObjectInfo
	authorize    func(string, string, string) error
	authorized   [][3]string
	copyAttempts int
	completed    int
	aborted      int
	statCalls    int
	removed      bool
	deleteErr    error
	onCopy       func(context.Context) error
}

func newCommandClient() (*commandClient, transfers.FileTransferRequest) {
	source := minio.ObjectInfo{ETag: "source-etag", Size: 6, LastModified: time.Date(2026, time.October, 20, 10, 0, 0, 0, time.UTC), ContentType: "text/plain"}
	body := transfers.FileTransferRequest{SourceBucket: "source-bucket", SourceKey: "folder/file.txt", DestinationBucket: "target-bucket", DestinationKey: "copies/file.txt", DestinationRevision: "absent"}
	body.SourceRevision = objectstate.DeletionRevision(objectstate.DeletionSnapshot{ETag: source.ETag, Size: source.Size, Modified: source.LastModified})
	return &commandClient{source: source}, body
}

func (c *commandClient) AuthorizeResource(_ context.Context, scope, bucket, key string) error {
	c.authorized = append(c.authorized, [3]string{scope, bucket, key})
	if c.authorize != nil {
		return c.authorize(scope, bucket, key)
	}
	return nil
}

func (c *commandClient) StatObject(_ context.Context, bucket, key string, _ minio.StatObjectOptions) (minio.ObjectInfo, error) {
	c.statCalls++
	if bucket == "source-bucket" && key == "folder/file.txt" && !c.removed {
		return c.source, nil
	}
	if bucket == "target-bucket" && c.destination != nil {
		return *c.destination, nil
	}
	return minio.ObjectInfo{}, minio.ErrorResponse{Code: "NoSuchKey", StatusCode: 404}
}

func (c *commandClient) GetObjectTagging(context.Context, string, string, minio.GetObjectTaggingOptions) (*tags.Tags, error) {
	return tags.NewTags(map[string]string{}, true)
}

func (c *commandClient) NewMultipartUpload(context.Context, string, string, minio.PutObjectOptions) (string, error) {
	return "copy-upload", nil
}

func (c *commandClient) CopyObjectPart(ctx context.Context, _, _, _, _, _ string, number int, _, _ int64, _ map[string]string) (minio.CompletePart, error) {
	c.copyAttempts++
	if c.onCopy != nil {
		if err := c.onCopy(ctx); err != nil {
			return minio.CompletePart{}, err
		}
	}
	return minio.CompletePart{PartNumber: number, ETag: "part-etag"}, nil
}

func (c *commandClient) CompleteMultipartUpload(context.Context, string, string, string, []minio.CompletePart, minio.PutObjectOptions) (minio.UploadInfo, error) {
	c.completed++
	c.destination = &minio.ObjectInfo{ETag: "destination-etag", Size: c.source.Size}
	return minio.UploadInfo{ETag: "destination-etag", Size: c.source.Size}, nil
}

func (c *commandClient) AbortMultipartUpload(context.Context, string, string, string) error {
	c.aborted++
	return nil
}

func (c *commandClient) ConditionalDeleteSupported() bool { return true }

func (c *commandClient) RemoveConditional(context.Context, string, string, string) error {
	if c.deleteErr != nil {
		return c.deleteErr
	}
	c.removed = true
	return nil
}

func commandScope() commandlog.Scope {
	return commandlog.Scope{ActorID: "actor", ConnectionID: "connection", Operation: "objects/transfer"}
}

func TestTransferCommandReplaysOnlyTheOriginalAuthorizedResult(t *testing.T) {
	repo := commandlog.NewMemory()
	t.Cleanup(func() { _ = repo.Close() })
	client, body := newCommandClient()
	first, id, replay, err := transfers.TransferCommand(context.Background(), repo, client, commandScope(), "copy-1", body)
	if err != nil || replay || id == "" || !first.Copied || first.Moved || client.copyAttempts != 1 || client.completed != 1 {
		t.Fatal("first command", first, id, replay, err)
	}
	statCalls := client.statCalls
	second, againID, replay, err := transfers.TransferCommand(context.Background(), repo, client, commandScope(), "copy-1", body)
	if err != nil || !replay || id != againID || !second.Copied || second.Moved || messages.Identity(first.Message) != messages.Identity(second.Message) || client.copyAttempts != 1 || client.statCalls != statCalls {
		t.Fatal("replay changed result or called storage", second, replay, err)
	}
	changed := body
	changed.DestinationKey = "copies/other.txt"
	if _, _, _, err = transfers.TransferCommand(context.Background(), repo, client, commandScope(), "copy-1", changed); apperror.Public(err).Code != "IdempotencyConflict" || client.copyAttempts != 1 {
		t.Fatal("same key accepted another request", err)
	}
	for _, denied := range []string{"objects:read", "objects:write"} {
		client.authorize = func(scope, _, _ string) error {
			if scope == denied {
				return authstate.Denied()
			}
			return nil
		}
		if _, replayID, replayed, err := transfers.TransferCommand(context.Background(), repo, client, commandScope(), "copy-1", body); apperror.Public(err).Status != 403 || replayID != "" || replayed || client.copyAttempts != 1 {
			t.Fatal("revoked or narrowed access disclosed old result", denied, replayID, replayed, err)
		}
	}
}

func TestTransferCommandLocaleDoesNotChangeExecutionOrPersistText(t *testing.T) {
	repo := commandlog.NewMemory()
	t.Cleanup(func() { _ = repo.Close() })
	client, body := newCommandClient()
	results := map[string]string{}
	for _, lang := range []string{"en", "ru"} {
		ctx := requestctx.WithLanguage(context.Background(), lang, true, localization.Default().For(lang))
		result, _, replayed, err := transfers.TransferCommand(ctx, repo, client, commandScope(), "one-copy", body)
		if err != nil || replayed != (lang == "ru") {
			t.Fatal(lang, replayed, err)
		}
		w := httptest.NewRecorder()
		common.WriteJSON(w, httptest.NewRequest("POST", "/", nil).WithContext(ctx), 200, result)
		var response struct {
			Copied, Moved bool
			Message       string
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || w.Code != 200 || !response.Copied || response.Moved || response.Message == "" || w.Header().Get("Content-Language") != lang {
			t.Fatal(lang, w.Body.String(), err)
		}
		results[lang] = response.Message
	}
	if results["en"] == results["ru"] || client.copyAttempts != 1 {
		t.Fatal("language changed execution or translation missing", results, client.copyAttempts)
	}
	input, _ := json.Marshal(body)
	keyHash, fingerprint := sha256.Sum256([]byte("one-copy")), sha256.Sum256(input)
	entry, _, err := repo.Reserve(context.Background(), commandScope(), hex.EncodeToString(keyHash[:]), hex.EncodeToString(fingerprint[:]), time.Now())
	if err != nil || entry.Response == nil || entry.Response.MessageID != string(messages.TransfersTheCopyWasCreated) {
		t.Fatal("neutral descriptor missing", entry, err)
	}
	stored, _ := json.Marshal(entry.Response)
	if strings.Contains(string(stored), results["en"]) || strings.Contains(string(stored), results["ru"]) || string(entry.Response.Payload) != `{"copied":true,"moved":false}` {
		t.Fatal("localized text entered persistent result", string(stored))
	}
}

func TestTransferCommandFailurePanicAndCancellationNeverReexecute(t *testing.T) {
	for _, scenario := range []string{"upstream error", "canceled", "panic"} {
		t.Run(scenario, func(t *testing.T) {
			repo := commandlog.NewMemory()
			t.Cleanup(func() { _ = repo.Close() })
			client, body := newCommandClient()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			failure := errors.New("private upstream failure")
			client.onCopy = func(context.Context) error {
				switch scenario {
				case "panic":
					panic("fixture panic")
				case "canceled":
					cancel()
					return ctx.Err()
				default:
					return failure
				}
			}
			panicked := false
			func() {
				defer func() { panicked = recover() != nil }()
				_, _, _, err := transfers.TransferCommand(ctx, repo, client, commandScope(), "uncertain-copy", body)
				if err == nil {
					t.Error("failed upstream command succeeded")
				}
			}()
			if panicked != (scenario == "panic") || client.copyAttempts != 1 || client.aborted != 1 {
				t.Fatal("panic/copy cleanup changed", panicked, client.copyAttempts, client.aborted)
			}
			if _, _, replayed, err := transfers.TransferCommand(context.Background(), repo, client, commandScope(), "uncertain-copy", body); apperror.Public(err).Code != "CommandOutcomeUncertain" || replayed || client.copyAttempts != 1 {
				t.Fatal("failed command replayed", replayed, err, client.copyAttempts)
			}
		})
	}
}

func TestTransferCommandPreservesPartialMoveWithoutRepeatingIt(t *testing.T) {
	repo := commandlog.NewMemory()
	t.Cleanup(func() { _ = repo.Close() })
	client, body := newCommandClient()
	body.Move = true
	client.deleteErr = apperror.New(502, "DeleteUncertain", messages.TransfersDeleteUncertain)
	for attempt := range 2 {
		result, _, replayed, err := transfers.TransferCommand(context.Background(), repo, client, commandScope(), "partial-move", body)
		if err != nil || !result.Copied || result.Moved || result.Code != "DeleteUncertain" || replayed != (attempt == 1) || client.copyAttempts != 1 {
			t.Fatal("partial move changed or repeated", result, replayed, err)
		}
	}
}

func TestTransferCommandRejectsMalformedKeysBeforeAuthorization(t *testing.T) {
	for _, key := range []string{"has space", "line\nbreak", "\x00", "\x7f", "ключ", strings.Repeat("a", 129)} {
		t.Run(fmt.Sprintf("%q", key), func(t *testing.T) {
			client, body := newCommandClient()
			if _, id, _, err := transfers.TransferCommand(context.Background(), nil, client, commandScope(), key, body); apperror.Public(err).Code != "InvalidIdempotencyKey" || id != "" || len(client.authorized) != 0 || client.copyAttempts != 0 {
				t.Fatal("invalid key reached storage or authorization", id, err)
			}
		})
	}
	client, body := newCommandClient()
	if result, id, replayed, err := transfers.TransferCommand(context.Background(), nil, client, commandScope(), "", body); err != nil || id != "" || replayed || !result.Copied || client.copyAttempts != 1 {
		t.Fatal("unkeyed legacy behavior changed", result, id, replayed, err)
	}
}

func TestAuthorizeTransferValidatesMappingAndEveryRequiredResource(t *testing.T) {
	client, body := newCommandClient()
	for _, bad := range []transfers.FileTransferRequest{{}, {SourceBucket: body.SourceBucket, SourceKey: "a/../file", DestinationBucket: body.DestinationBucket, DestinationKey: "file"}, {SourceBucket: body.SourceBucket, SourceKey: "file", DestinationBucket: body.SourceBucket, DestinationKey: "file"}} {
		if err := transfers.AuthorizeTransfer(context.Background(), client, bad); apperror.Public(err).Status != 400 || len(client.authorized) != 0 {
			t.Fatal("invalid mapping reached authorization", bad, err)
		}
	}
	body.Move = true
	if err := transfers.AuthorizeTransfer(context.Background(), client, body); err != nil {
		t.Fatal(err)
	}
	want := [][3]string{{"objects:read", body.SourceBucket, body.SourceKey}, {"objects:write", body.DestinationBucket, body.DestinationKey}, {"objects:delete", body.SourceBucket, body.SourceKey}}
	if !reflect.DeepEqual(client.authorized, want) {
		t.Fatal("move authorization omitted or broadened resources", client.authorized)
	}
	client.authorize = func(scope, _, _ string) error {
		if scope == "objects:delete" {
			return authstate.Denied()
		}
		return nil
	}
	if err := transfers.AuthorizeTransfer(context.Background(), client, body); apperror.Public(err).Status != 403 {
		t.Fatal("move accepted without delete permission", err)
	}
}
