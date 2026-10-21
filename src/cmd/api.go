package cmd

import (
	"context"
	"log/slog"
	"os/signal"
	"syscall"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	app "github.com/owiks/golang-minio-s3-backend-api/src/http"
)

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	return app.Run(ctx, cfg, logger)
}
