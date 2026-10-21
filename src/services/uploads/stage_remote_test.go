package uploads

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/result"
	"minio-web/api/src/services/staging"
	"minio-web/api/src/services/uploads/source"
)

type sourceBody struct {
	io.Reader
	reads, closes int
}

type cleanupFailureBody struct {
	trigger func()
	done    bool
	fail    bool
}

func (r *cleanupFailureBody) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	r.done = true
	r.trigger()
	if r.fail {
		return 0, errors.New("source interrupted")
	}
	return copy(p, "body"), io.EOF
}
func (*cleanupFailureBody) Close() error { return nil }

func TestImportTransfersFailedCleanupWithoutChangingOperationResult(t *testing.T) {
	for _, scenario := range []string{"source failure", "successful import", "write failure"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("TMPDIR", dir)
			budget := staging.New(4)
			var name, blocker string
			body := &cleanupFailureBody{fail: scenario == "source failure", trigger: func() {
				files, err := filepath.Glob(filepath.Join(dir, "minio-import-*"))
				if err != nil || len(files) != 1 {
					t.Fatal("staging file not owned by shared budget", files, err)
				}
				name = files[0]
				if err := os.Remove(name); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(name, 0700); err != nil {
					t.Fatal(err)
				}
				blocker = filepath.Join(name, "blocker")
				if err := os.WriteFile(blocker, []byte("test"), 0600); err != nil {
					t.Fatal(err)
				}
			}}
			t.Cleanup(func() { _ = os.Remove(blocker); _ = os.Remove(name); _ = budget.Close() })
			remote := importSourceFunc(func(context.Context, string) (source.File, error) {
				return source.File{Body: body, Size: 4, ContentType: "text/plain"}, nil
			})
			destination := &conditionalDestination{lookupErr: minio.ErrorResponse{Code: "NoSuchKey"}}
			if scenario == "write failure" {
				destination.writeErr = minio.ErrorResponse{Code: "AccessDenied"}
			}
			_, err := Import(context.Background(), budget, remote, &PreparedImport{client: destination, bucket: "test-bucket", key: "file.txt", url: "https://example.org/file", revision: "absent"}, func(result.StreamEvent[ImportResult]) {})
			want := ""
			switch scenario {
			case "source failure":
				want = "IncompleteSource"
			case "write failure":
				want = "AccessDenied"
			}
			if (want == "" && err != nil) || (want != "" && (err == nil || apperror.Public(err).Code != want)) {
				t.Fatal("cleanup masked the operation result", err)
			}
			if budget.PendingCleanup() != (staging.CleanupStats{Files: 1, Bytes: 4}) || budget.Used() != 4 {
				t.Fatal("deferred Close lost failed resource")
			}
			if scenario == "source failure" && destination.writes != 0 {
				t.Fatal("incomplete source was uploaded")
			}
			if err := os.Remove(blocker); err != nil {
				t.Fatal(err)
			}
			budget.Reap(time.Now().Add(time.Minute))
			if budget.PendingCleanup().Files != 0 || budget.Used() != 0 {
				t.Fatal("import resource was not reaped")
			}
			if _, err := os.Stat(name); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("staging path not removed", err)
			}
		})
	}
}

func (r *sourceBody) Read(p []byte) (int, error) { r.reads++; return r.Reader.Read(p) }
func (r *sourceBody) Close() error               { r.closes++; return nil }

type failedSourceReader struct{}

func (failedSourceReader) Read(p []byte) (int, error) {
	return copy(p, "x"), errors.New("source disconnected")
}

func TestStagingUsesOnlySourcePortAndOwnsResources(t *testing.T) {
	for _, tc := range []struct {
		name, body, code  string
		size, budget      int64
		readError, cancel bool
	}{
		{name: "success", body: "body", size: 4, budget: 4},
		{name: "empty", size: 0, budget: 0},
		{name: "unknown-size", body: "body", size: -1, budget: MaxRemoteSize},
		{name: "incomplete", body: "body", size: 8, budget: 8, code: "IncompleteSource"},
		{name: "read-error", size: 4, budget: 4, readError: true, code: "IncompleteSource"},
		{name: "over-declared-size", body: "longer", size: 4, budget: 4, code: "TemporaryStorageFull"},
		{name: "oversized", size: MaxRemoteSize + 1, budget: MaxRemoteSize, code: "ImportTooLarge"},
		{name: "budget-exhausted", body: "body", size: 4, budget: 3, code: "TemporaryStorageFull"},
		{name: "unknown-size-reserves-limit", body: "body", size: -1, budget: 3, code: "TemporaryStorageFull"},
		{name: "cancelled", body: "body", size: 4, budget: 4, cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var reader io.Reader = strings.NewReader(tc.body)
			if tc.readError {
				reader = failedSourceReader{}
			}
			body := &sourceBody{Reader: reader}
			calls := 0
			remote := importSourceFunc(func(received context.Context, raw string) (source.File, error) {
				calls++
				if received != ctx || raw != "https://example.org/file?token=signed" {
					t.Fatal("source context or URL changed")
				}
				return source.File{Body: body, Size: tc.size, ContentType: "application/json"}, nil
			})
			if tc.cancel {
				cancel()
			}
			budget := staging.New(tc.budget)
			var lastCount, lastTotal int64
			file, size, contentType, err := StageRemote(ctx, remote, "https://example.org/file?token=signed", func(n, total int64) { lastCount, lastTotal = n, total }, budget)
			if calls != 1 || body.closes != 1 {
				t.Fatal("source retried or body not closed", calls, body.closes)
			}
			if tc.code != "" || tc.cancel {
				if tc.cancel {
					if !errors.Is(err, context.Canceled) || body.reads != 0 {
						t.Fatal("cancelled source read", err, body.reads)
					}
				} else if err == nil || apperror.Public(err).Code != tc.code {
					t.Fatal("staging error changed", err, tc.code)
				}
				if file != nil || size != 0 || contentType != "" || budget.Used() != 0 {
					t.Fatal("failure leaked staging or reported success")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			name := file.Name()
			defer file.Close()
			data, err := io.ReadAll(file)
			if err != nil || string(data) != tc.body || size != int64(len(tc.body)) || contentType != "application/json" || lastCount != size || lastTotal != size {
				t.Fatal("staged bytes, progress or metadata changed", err)
			}
			reservation := tc.size
			if reservation < 0 {
				reservation = MaxRemoteSize
			}
			if budget.Used() != reservation {
				t.Fatal("staging reservation lost")
			}
			if err := file.Close(); err != nil || budget.Used() != 0 {
				t.Fatal("staging reservation not released", err)
			}
			if _, err := os.Stat(name); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("temporary file survived close", err)
			}
		})
	}
}

func TestSourceFailurePreventsStagingAndWrites(t *testing.T) {
	failure := apperror.New(502, "SourceUnavailable", "safe source error")
	budget := staging.New(4)
	remote := importSourceFunc(func(context.Context, string) (source.File, error) { return source.File{}, failure })
	file, size, contentType, err := StageRemote(context.Background(), remote, "https://example.org/file", func(int64, int64) { t.Fatal("failed source reported progress") }, budget)
	if !errors.Is(err, failure) || file != nil || size != 0 || contentType != "" || budget.Used() != 0 {
		t.Fatal("source error changed or allocated staging", err)
	}
	destination := &conditionalDestination{}
	_, err = Import(context.Background(), budget, remote, &PreparedImport{client: destination, bucket: "bucket", key: "file.txt", url: "https://example.org/file", revision: "absent"}, func(result.StreamEvent[ImportResult]) {})
	if !errors.Is(err, failure) || destination.lookups != 0 || destination.writes != 0 || budget.Used() != 0 {
		t.Fatal("failed source reached destination", err)
	}
}
