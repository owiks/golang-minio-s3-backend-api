package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	httpcommon "minio-web/api/src/http/api/common"
	accesssvc "minio-web/api/src/services/access"
	runtimesvc "minio-web/api/src/services/runtime"
	"minio-web/api/tests/testsession"
)

func TestS3FallbackConfirmsNamesWithoutCachedLocation(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		t.Run(fmt.Sprint(unavailable), func(t *testing.T) {
			var lists atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Error("permission discovery sent mutation", r.Method)
					w.WriteHeader(405)
					return
				}
				if strings.HasPrefix(r.URL.Path, "/minio/admin/") {
					httpcommon.WriteJSON(w, 501, map[string]string{"Code": "NotImplemented"})
					return
				}
				w.Header().Set("Content-Type", "application/xml")
				if r.URL.Path == "/" {
					fmt.Fprint(w, `<ListAllMyBucketsResult><Buckets><Bucket><Name>allowed-bucket</Name></Bucket><Bucket><Name>hidden-bucket</Name></Bucket></Buckets></ListAllMyBucketsResult>`)
					return
				}
				if r.URL.Query().Get("list-type") != "2" {
					t.Error("expected actual list request", r.URL.String())
					w.WriteHeader(400)
					return
				}
				lists.Add(1)
				if r.URL.Path == "/allowed-bucket/" || r.URL.Path == "/allowed-bucket" {
					fmt.Fprint(w, `<ListBucketResult><Name>allowed-bucket</Name><IsTruncated>false</IsTruncated></ListBucketResult>`)
					return
				}
				if unavailable {
					w.WriteHeader(503)
					fmt.Fprint(w, `<Error><Code>ServiceUnavailable</Code></Error>`)
				} else {
					w.WriteHeader(403)
					fmt.Fprint(w, `<Error><Code>AccessDenied</Code></Error>`)
				}
			}))
			defer upstream.Close()
			u, _ := url.Parse(upstream.URL)
			tr := http.DefaultTransport.(*http.Transport).Clone()
			defer tr.CloseIdleConnections()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			buckets, err := accesssvc.AccessibleBuckets(ctx, runtimesvc.NewClients(testsession.With(t, runtimesvc.Connection{Endpoint: u.String(), AccessKey: "fixture", SecretKey: "test-secret"}, testConfig(), tr)))
			if unavailable {
				if err == nil {
					t.Fatal("network failure was reported as known access", buckets)
				}
			} else if err != nil || len(buckets) != 1 || buckets[0].Name != "allowed-bucket" {
				t.Fatal("fallback exposed unverified name", buckets, err)
			}
			if lists.Load() != 2 {
				t.Fatal("did not verify both candidate buckets over HTTP", lists.Load())
			}
		})
	}
}

func TestAccessReadWorkersAreBoundedAndStopOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{}, 4)
	done := make(chan struct{})
	var active, maximum, calls atomic.Int32
	go func() {
		accesssvc.ParallelAccessReads(ctx, 10000, func(int) {
			calls.Add(1)
			n := active.Add(1)
			for old := maximum.Load(); n > old && !maximum.CompareAndSwap(old, n); old = maximum.Load() {
			}
			entered <- struct{}{}
			<-ctx.Done()
			active.Add(-1)
		})
		close(done)
	}()
	for range 4 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("workers did not start")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("workers ignored cancellation")
	}
	if maximum.Load() > 4 || active.Load() != 0 || calls.Load() != 4 {
		t.Fatal("unbounded work or leaked workers", maximum.Load(), active.Load(), calls.Load())
	}
}
