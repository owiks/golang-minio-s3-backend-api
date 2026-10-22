package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/database"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/migrations"
)

var errMigrationCommand = errors.New("use: api migrate up|status|version|down [--target] [--offline]")

func migrateCommand(ctx context.Context, args []string, output io.Writer) error {
	if len(args) == 0 {
		return errMigrationCommand
	}
	action := args[0]
	switch action {
	case "up", "status", "version", "down":
	default:
		return errMigrationCommand
	}
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	target := flags.Bool("target", false, "use API_TARGET_DB_* instead of API_DB_*")
	offline := flags.Bool("offline", false, "confirm all API writers are stopped, including other hosts; required for down and legacy adoption")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 {
		return errMigrationCommand
	}
	if action == "down" && !*offline {
		return errors.New("migrate down requires --offline with API writers stopped")
	}
	cfg, err := config.LoadMigrationDatabase(*target)
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
	if action == "up" && status.Legacy && !*offline {
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

func runMigrate(args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	return migrateCommand(ctx, args, os.Stdout)
}
