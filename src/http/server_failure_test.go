package http

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	stdhttp "net/http"
	"sync"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	httpapi "github.com/owiks/golang-minio-s3-backend-api/src/http/api"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/folders/share"
)

func TestServeFailureClosesAcceptedKeepAlive(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ended := make(chan error, 1)
	go func() {
		ended <- serve(context.Background(), httpapi.New(config.Default()), listener, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	input := bufio.NewReader(conn)
	_, err = io.WriteString(conn, "GET /health HTTP/1.1\r\nHost: localhost\r\n\r\n")
	if err != nil {
		t.Fatal(err)
	}
	response, err := stdhttp.ReadResponse(input, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	_ = listener.Close()
	select {
	case err := <-ended:
		if !errors.Is(err, ErrServe) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not stop")
	}
	_, err = io.WriteString(conn, "GET /health HTTP/1.1\r\nHost: localhost\r\n\r\n")
	if err == nil {
		if response, err := stdhttp.ReadResponse(input, nil); err == nil {
			_ = response.Body.Close()
			t.Fatal("accepted connection still serves after runtime disposal")
		}
	}
}

type cancelledFolderRead struct{ entered, cancelled, proceed chan struct{} }

func (f *cancelledFolderRead) StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error) {
	panic("unexpected stat")
}
func (f *cancelledFolderRead) GetObject(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error) {
	panic("unexpected get")
}
func (f *cancelledFolderRead) ListObjectPage(ctx context.Context, _ string, _ string, _ string, _ string, _ int) (minio.ListBucketV2Result, error) {
	close(f.entered)
	<-ctx.Done()
	close(f.cancelled)
	<-f.proceed
	return minio.ListBucketV2Result{}, ctx.Err()
}

func TestServeFailureCancelsAndJoinsHandlersBeforeRuntimeClose(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	handler := httpapi.New(config.Default())
	reader := &cancelledFolderRead{make(chan struct{}), make(chan struct{}), make(chan struct{})}
	var once sync.Once
	defer once.Do(func() { close(reader.proceed) })
	grant := foldershare.New(foldershare.Config{Owner: "owner", Token: "test-token", Bucket: "bucket-test", Expires: time.Now().Add(time.Hour), Reader: reader})
	if err := handler.Shares.Add(grant); err != nil {
		t.Fatal(err)
	}
	ended := make(chan error, 1)
	go func() {
		ended <- serve(context.Background(), handler, listener, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		r, _ := stdhttp.NewRequest("GET", "http://"+listener.Addr().String()+"/api/shared-folder?mode=list&prefix=", nil)
		r.Header.Set("Authorization", "Bearer test-token")
		response, err := stdhttp.DefaultClient.Do(r)
		if err == nil {
			_ = response.Body.Close()
		}
	}()
	select {
	case <-reader.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not enter delegated reader")
	}
	_ = listener.Close()
	select {
	case <-reader.cancelled:
	case <-time.After(time.Second):
		t.Fatal("Serve failure did not cancel active request")
	}
	select {
	case <-ended:
		t.Fatal("runtime disposed before handler released resources")
	default:
	}
	if handler.Shares.Get("test-token") == nil {
		t.Fatal("runtime closed with a running handler")
	}
	once.Do(func() { close(reader.proceed) })
	select {
	case err := <-ended:
		if !errors.Is(err, ErrServe) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not join cancelled handler")
	}
	if grant.ActiveReads() != 0 || handler.Shares.Get("test-token") != nil {
		t.Fatal("handler lease or runtime retained after exit")
	}
	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("client connection retained")
	}
}
