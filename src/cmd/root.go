package cmd

import (
	"log/slog"
	"os"
)

func Execute() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	if err := run(logger); err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
}
