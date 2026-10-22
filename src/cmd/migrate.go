package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/database"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/migrations"
)

var errMigrationCommand = errors.New("use: api migrate up|status|version|down [--target] [--offline]")

type migrationOptions struct {
	target  bool
	offline bool
}

func newMigrateCommand() *cobra.Command {
	command := &cobra.Command{
		Use: "migrate", Short: "Manage database schema with Goose",
		SilenceUsage: true, SilenceErrors: true,
		Args: noCommandArguments(errMigrationCommand),
		RunE: func(_ *cobra.Command, _ []string) error { return errMigrationCommand },
	}
	command.SetFlagErrorFunc(func(_ *cobra.Command, _ error) error { return errMigrationCommand })
	for _, action := range []struct{ name, description string }{
		{"up", "Apply pending migrations"},
		{"status", "Print migration status as JSON without changing the schema"},
		{"version", "Print the current schema version"},
		{"down", "Roll back one migration; all API writers must be stopped"},
	} {
		command.AddCommand(newMigrationAction(action.name, action.description))
	}
	return command
}

func newMigrationAction(action, description string) *cobra.Command {
	options := migrationOptions{}
	command := &cobra.Command{
		Use: action, Short: description,
		Args: noCommandArguments(errMigrationCommand),
		RunE: maintenanceRun(func(ctx context.Context, output io.Writer) error {
			return executeMigration(ctx, action, options, output)
		}),
	}
	command.SetFlagErrorFunc(func(_ *cobra.Command, _ error) error { return errMigrationCommand })
	command.Flags().BoolVar(&options.target, "target", false, "Use API_TARGET_DB_* instead of API_DB_*")
	command.Flags().BoolVar(&options.offline, "offline", false, "Confirm all API writers on every host are stopped")
	return command
}

func migrateCommand(ctx context.Context, args []string, output io.Writer) error {
	command := newMigrateCommand()
	command.SetArgs(append([]string{}, args...))
	command.SetOut(output)
	command.SetErr(io.Discard)
	return command.ExecuteContext(ctx)
}

func executeMigration(ctx context.Context, action string, options migrationOptions, output io.Writer) error {
	if action == "down" && !options.offline {
		return errors.New("migrate down requires --offline with API writers stopped")
	}
	cfg, err := config.LoadMigrationDatabase(options.target)
	if err != nil {
		return err
	}
	if cfg.Driver == "memory" {
		if action == "version" {
			_, err := fmt.Fprintln(output, 0)
			return err
		}
		return json.NewEncoder(output).Encode(migrations.Status{Entries: []migrations.Entry{}})
	}
	// Migration locks and Goose may need separate SQL connections. This isolated
	// maintenance pool is independent of the API request pool's configured limit.
	db, err := database.Open(ctx, database.Config{Database: cfg, MaxOpen: 4, MaxIdle: 2, Lifetime: time.Minute})
	if err != nil {
		return err
	}
	defer db.Close()
	r, err := migrations.New(db, cfg.Driver, cfg.SQLitePath)
	if err != nil {
		return err
	}
	status, err := r.Status(ctx)
	if err != nil {
		return err
	}
	if action == "up" && status.Legacy && !options.offline {
		return errors.New("legacy schema adoption requires migrate up --offline with API writers stopped")
	}
	switch action {
	case "up":
		err = r.Up(ctx)
	case "down":
		err = r.Down(ctx)
	case "version":
		_, err = fmt.Fprintln(output, status.Current)
		return err
	}
	if err != nil {
		return err
	}
	status, err = r.Status(ctx)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(status)
}
