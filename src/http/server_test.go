package http

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"minio-web/api/src/config"
	httpapi "minio-web/api/src/http/api"
)

func TestServeHealthAndGracefulCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		finished <- serve(ctx, httpapi.New(config.Default()), listener, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get("http://" + listener.Addr().String() + "/health")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || string(raw) != "{\"ok\":true}\n" || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("health contract changed: status=%d body=%s error=%v", response.StatusCode, raw, err)
	}
	cancel()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not release server and reaper")
	}
	if response, err := client.Get("http://" + listener.Addr().String() + "/health"); err == nil {
		_ = response.Body.Close()
		t.Fatal("listener still accepts connections after shutdown")
	}
}

func TestStartupAndServeFailuresAreSafe(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Default()
	cfg.Listen = "invalid-secret-value"
	if err := Run(context.Background(), cfg, logger); !errors.Is(err, ErrListen) {
		t.Fatalf("unexpected startup result: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener.Close()
	if err := serve(context.Background(), httpapi.New(config.Default()), listener, logger); !errors.Is(err, ErrServe) {
		t.Fatalf("unexpected serve result: %v", err)
	}
}
