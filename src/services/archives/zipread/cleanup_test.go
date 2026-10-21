package zipread

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/staging"
)

func TestZIPLoadFailureHandsCleanupToCallerBudget(t *testing.T) {
	for _, incomplete := range []bool{false, true} {
		t.Run(fmt.Sprint("incomplete=", incomplete), func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("TMPDIR", dir)
			budget := staging.New(32)
			t.Cleanup(func() { _ = budget.Close() })
			prepared := make(chan error, 1)
			blocker := ""
			name := ""
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("ETag", `"fixture"`)
				w.Header().Set("Last-Modified", time.Unix(1700000000, 0).UTC().Format(http.TimeFormat))
				w.Header().Set("Content-Length", "32")
				if r.Method == "HEAD" {
					return
				}
				prepare := func() error {
					paths, err := filepath.Glob(filepath.Join(dir, "minio-zip-*"))
					if err != nil || len(paths) != 1 {
						return fmt.Errorf("expected one staged ZIP, got %d", len(paths))
					}
					name = paths[0]
					if err := os.Remove(name); err != nil {
						return err
					}
					if err := os.Mkdir(name, 0700); err != nil {
						return err
					}
					blocker = filepath.Join(name, "blocker")
					return os.WriteFile(blocker, []byte("test"), 0600)
				}
				prepared <- prepare()
				size := 32
				if incomplete {
					size = 16
				}
				w.Header().Set("Content-Length", strconv.Itoa(size))
				_, _ = w.Write([]byte(strings.Repeat("x", size)))
			}))
			defer srv.Close()
			client, err := minio.New(strings.TrimPrefix(srv.URL, "http://"), &minio.Options{Creds: credentials.NewStaticV4("fixture", "secret", ""), Region: "us-east-1", MaxRetries: 1})
			if err != nil {
				t.Fatal(err)
			}
			f, reader, rev, err := LoadStoredZIP(context.Background(), client, "test-bucket", "invalid.zip", budget)
			if prepareErr := <-prepared; prepareErr != nil {
				t.Fatal(prepareErr)
			}
			t.Cleanup(func() { _ = os.Remove(blocker); _ = os.Remove(name) })
			want := "InvalidZIP"
			if incomplete {
				want = "ZIPChanged"
			}
			if err == nil || apperror.Public(err).Code != want || f != nil || reader != nil || rev != "" {
				t.Fatal("cleanup masked ZIP failure", err)
			}
			if budget.PendingCleanup() != (staging.CleanupStats{Files: 1, Bytes: 32}) || budget.Used() != 32 {
				t.Fatal("ZIP failure lost cleanup owner", budget.PendingCleanup())
			}
			if err := os.Remove(blocker); err != nil {
				t.Fatal(err)
			}
			budget.Reap(time.Now().Add(time.Minute))
			if budget.Used() != 0 || budget.PendingCleanup().Files != 0 {
				t.Fatal("ZIP failure leaked reservation")
			}
			if _, err := os.Stat(name); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("ZIP path not removed", err)
			}
		})
	}
}
