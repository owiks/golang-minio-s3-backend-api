// Package http composes the HTTP API and owns its lifecycle.
package http

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	httpapi "github.com/owiks/golang-minio-s3-backend-api/src/http/api"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/middleware"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/presentation"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/requestctx"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

var (
	ErrListen = errors.New("cannot listen for API requests")
	ErrServe  = errors.New("API stopped unexpectedly")
)

// Run returns only errors safe to log, never listener or upstream error text.
func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	manager, err := persistence.Open(startup, cfg)
	cancel()
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		_ = manager.Close()
		return ErrListen
	}
	return serve(ctx, httpapi.NewHandler(runtime.NewWithAuth(cfg, manager)), listener, logger)
}

func serve(ctx context.Context, handler *httpapi.Handler, listener net.Listener, logger *slog.Logger) error {
	defer listener.Close()
	reaperContext, stopReaper := context.WithCancel(ctx)
	reaped := make(chan struct{})
	go func() {
		defer close(reaped)
		handler.Reap(reaperContext)
	}()
	defer func() {
		stopReaper()
		<-reaped
		handler.Close()
	}()
	metricsDone := make(chan struct{})
	go func() {
		defer close(metricsDone)
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()
		for {
			select {
			case <-reaperContext.Done():
				handler.Metrics.LogInterval(logger)
				return
			case <-tick.C:
				handler.Metrics.LogInterval(logger)
			}
		}
	}()
	defer func() { stopReaper(); <-metricsDone }()
	srv := &http.Server{
		Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 * 1024,
	}
	// Shutdown allows active requests to drain. Every exit, including Serve
	// failure, then closes accepted connections and joins their handlers before
	// disposing runtime. The gate prevents Add from racing the final Wait.
	requests := &requestGroup{}
	requestContext, cancelRequests := context.WithCancel(context.Background())
	srv.BaseContext = func(net.Listener) context.Context { return requestContext }
	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !requests.begin() {
			writeStoppingResponse(w, r, handler.Config().Language)
			return
		}
		defer requests.active.Done()
		handler.ServeHTTP(w, r)
	})
	defer func() {
		requests.stop()
		cancelRequests()
		_ = srv.Close()
		requests.active.Wait()
	}()
	stopped := make(chan error, 1)
	go func() { stopped <- srv.Serve(listener) }()
	logger.Info("API started", "address", listener.Addr().String())
	select {
	case err := <-stopped:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return ErrServe
		}
	case <-ctx.Done():
		logger.Info("API draining")
		shutdown, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdown); err != nil {
			logger.Warn("API shutdown deadline exceeded")
			_ = srv.Close()
		}
		<-stopped
	}
	logger.Info("API stopped")
	return nil
}

func writeStoppingResponse(w http.ResponseWriter, r *http.Request, mode string) {
	r = middleware.LocalizeRequest(r, mode, nil)
	presentation.LanguageHeaders(r.Context(), w.Header())
	// Preserve the existing status, plain-text envelope and trailing newline.
	http.Error(w, requestctx.Localizer(r.Context()).Render(messages.HTTPStopping), http.StatusServiceUnavailable)
}

type requestGroup struct {
	mu     sync.Mutex
	closed bool
	active sync.WaitGroup
}

func (g *requestGroup) begin() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return false
	}
	g.active.Add(1)
	return true
}

func (g *requestGroup) stop() {
	g.mu.Lock()
	g.closed = true
	g.mu.Unlock()
}
