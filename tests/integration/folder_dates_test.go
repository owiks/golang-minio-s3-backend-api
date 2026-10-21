package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"

	objectssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/objects"
	storagesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
)

func TestFolderDatesOnlyUseExactMarkers(t *testing.T) {
	modified := time.Date(2026, 10, 19, 9, 15, 0, 0, time.UTC)
	var reads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		if r.Method != "HEAD" {
			t.Errorf("unexpected recursive read: %s", r.Method)
		}
		switch r.URL.Path {
		case "/fixture/created/":
			w.Header().Set("Last-Modified", modified.Format(http.TimeFormat))
			w.Header().Set("Content-Length", "0")
		case "/fixture/denied/":
			w.WriteHeader(http.StatusForbidden)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	client, err := minio.New(strings.TrimPrefix(srv.URL, "http://"), &minio.Options{Region: "us-east-1", BucketLookup: minio.BucketLookupPath, MaxRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	rows := []objectssvc.Entry{{Key: "created/", Kind: "folder"}, {Key: "virtual/", Kind: "folder"}, {Key: "denied/", Kind: "folder"}, {Key: "file.txt", Kind: "file", Modified: &modified}}
	objectssvc.FillFolderDates(context.Background(), storagesvc.NewS3Client(client), "fixture", rows)
	if rows[0].Modified == nil || !rows[0].Modified.Equal(modified) {
		t.Fatalf("marker date missing: %+v", rows)
	}
	if rows[1].Modified != nil || rows[2].Modified != nil {
		t.Fatalf("invented date: %+v", rows)
	}
	if rows[3].Modified != &modified || reads.Load() != 3 {
		t.Fatalf("file metadata changed or extra reads: %d", reads.Load())
	}
}

func TestFolderDatesBoundConcurrencyAndRespectCancellation(t *testing.T) {
	var running, peak, calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		n := running.Add(1)
		defer running.Add(-1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		<-r.Context().Done()
	}))
	defer srv.Close()
	client, err := minio.New(strings.TrimPrefix(srv.URL, "http://"), &minio.Options{Region: "us-east-1", BucketLookup: minio.BucketLookupPath, MaxRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]objectssvc.Entry, 20)
	for i := range rows {
		rows[i] = objectssvc.Entry{Key: "slow/", Kind: "folder"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	objectssvc.FillFolderDates(ctx, storagesvc.NewS3Client(client), "fixture", rows)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("metadata delayed listing: %s", elapsed)
	}
	if peak.Load() > 4 || calls.Load() > 4 || calls.Load() == 0 {
		t.Fatalf("unbounded calls: peak=%d calls=%d", peak.Load(), calls.Load())
	}
	for _, row := range rows {
		if row.Modified != nil {
			t.Fatal("date survived cancellation")
		}
	}
}
