package cmd

import (
	"log/slog"
	"os"
)

func Execute() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	runCommand := func() error {
		if len(os.Args) > 1 {
			switch os.Args[1] {
			case "state":
				return runState(os.Args[2:])
			case "migrate":
				return runMigrate(os.Args[2:])
			default:
				return errMigrationCommand
			}
		}
		return run(logger)
	}
	if err := runCommand(); err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
}
