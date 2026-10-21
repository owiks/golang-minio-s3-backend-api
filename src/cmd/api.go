package cmd

import (
	"context"
	"log/slog"
	"os/signal"
	"syscall"

	"minio-web/api/src/config"
	app "minio-web/api/src/http"
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
