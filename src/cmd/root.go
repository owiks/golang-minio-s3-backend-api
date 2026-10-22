package cmd

import (
	"context"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"
)

func Execute() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	command := newRootCommand(func() error { return run(logger) }, os.Stdout, os.Stderr)
	if err := command.Execute(); err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
}

// Each invocation owns its command tree, flag values and output writers.
func newRootCommand(start func() error, output, errorOutput io.Writer) *cobra.Command {
	command := &cobra.Command{
		Use:           "api",
		Short:         "Run the S3 API or maintain its persistent state",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noCommandArguments(errMigrationCommand),
		RunE: func(_ *cobra.Command, _ []string) error {
			return start()
		},
	}
	command.SetOut(output)
	command.SetErr(errorOutput)
	command.CompletionOptions.DisableDefaultCmd = true
	command.SetFlagErrorFunc(func(_ *cobra.Command, _ error) error { return errMigrationCommand })
	command.AddCommand(newStateCommand(), newMigrateCommand())
	return command
}

func noCommandArguments(invalid error) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != 0 {
			return invalid
		}
		return nil
	}
}

func maintenanceRun(run func(context.Context, io.Writer) error) func(*cobra.Command, []string) error {
	return func(command *cobra.Command, _ []string) error {
		ctx, cancel := context.WithTimeout(command.Context(), 5*time.Minute)
		defer cancel()
		return run(ctx, command.OutOrStdout())
	}
}
