package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/secretbox"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
)

var errStateCommand = errors.New("invalid state command; use: state keygen|key-add|migrate|verify|backup|restore|transfer|reencrypt|prune")

func stateCommand(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errStateCommand
	}
	name := args[0]
	// Compatibility alias: migration remains an explicit command, never part of Open.
	if name == "migrate" {
		return migrateCommand(ctx, append([]string{"up"}, args[1:]...), os.Stdout)
	}
	flags := flag.NewFlagSet("state "+name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	before := flags.String("before", "", "absolute expiry retention boundary, RFC3339")
	out := flags.String("out", "", "new output file")
	in := flags.String("in", "", "input backup")
	keyfile := flags.String("file", "", "keyring file")
	offline := flags.Bool("offline", false, "API writers are stopped")
	targetKeyring := flags.String("target-keyring-file", "", "destination keyring file")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 {
		return errStateCommand
	}
	if name == "keygen" || name == "key-add" {
		ring, err := secretbox.Generate()
		if err != nil {
			return err
		}
		if name == "keygen" {
			if *out == "" {
				return errStateCommand
			}
			return writeJSONFile(*out, ring, false)
		}
		if *keyfile == "" {
			return errStateCommand
		}
		data, err := readBounded(*keyfile, 65536)
		if err != nil {
			return err
		}
		var existing secretbox.Keyring
		if json.Unmarshal(data, &existing) != nil {
			return errStateCommand
		}
		if _, err = secretbox.New(existing); err != nil {
			return err
		}
		id := "key-" + securetoken.New()
		existing.Keys[id] = ring.Keys[ring.Active]
		existing.Active = id
		if _, err = secretbox.New(existing); err != nil {
			return err
		}
		return writeJSONFile(*keyfile, existing, true)
	}
	switch name {
	case "verify", "backup", "restore", "transfer", "reencrypt", "prune":
	default:
		return errStateCommand
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.State.Driver == "memory" {
		return errors.New("state commands require a persistent API_DB_DRIVER")
	}
	m, err := persistence.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer m.Close()
	switch name {
	case "verify":
		return m.Verify(ctx)
	case "backup":
		if *out == "" {
			return errStateCommand
		}
		b, err := m.Export(ctx)
		if err != nil {
			return err
		}
		return writeJSONFile(*out, b, false)
	case "restore":
		if *in == "" || !*offline {
			return errors.New("restore requires --in and --offline with API writers stopped")
		}
		raw, err := readBounded(*in, 64*1024*1024)
		if err != nil {
			return err
		}
		defer clear(raw)
		var b authstate.Backup
		if json.Unmarshal(raw, &b) != nil {
			return errStateCommand
		}
		return m.Restore(ctx, b, false)
	case "transfer":
		if !*offline {
			return errors.New("transfer requires --offline and API_TARGET_DB_* connection fields")
		}
		database, err := config.LoadTransferDatabase()
		if err != nil {
			return err
		}
		if database.Driver == "memory" {
			return errors.New("transfer requires a persistent API_TARGET_DB_DRIVER")
		}
		b, err := m.Export(ctx)
		if err != nil {
			return err
		}
		target := cfg
		target.State.Database = database
		if *targetKeyring != "" {
			target.State.KeyringFile = *targetKeyring
		}
		dest, err := persistence.Open(ctx, target)
		if err != nil {
			return err
		}
		defer dest.Close()
		return dest.Restore(ctx, b, true)
	case "prune":
		boundary, err := time.Parse(time.RFC3339, *before)
		if err != nil {
			return errStateCommand
		}
		n, err := m.Prune(ctx, boundary)
		if err == nil {
			fmt.Fprintf(os.Stdout, "Pruned records: %d\n", n)
		}
		return err
	case "reencrypt":
		n, err := m.Reencrypt(ctx)
		if err == nil {
			fmt.Fprintf(os.Stdout, "Reencrypted connections: %d\n", n)
		}
		return err
	}
	return errStateCommand
}
func readBounded(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot read state input file")
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return nil, errors.New("invalid or oversized state input file")
	}
	return raw, nil
}

// Write privately and publish atomically. New backups never overwrite a file.
func writeJSONFile(path string, value any, replace bool) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil || len(raw) > 64*1024*1024 {
		return errors.New("cannot encode state file within 64 MiB")
	}
	defer clear(raw)
	f, err := os.CreateTemp(filepath.Dir(path), ".api-state-*")
	if err != nil {
		return errors.New("cannot create private state file")
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if f.Chmod(0600) != nil {
		return errors.New("cannot secure state file")
	}
	if _, err = f.Write(append(raw, '\n')); err != nil {
		return errors.New("cannot write state file")
	}
	if f.Sync() != nil || f.Close() != nil {
		return errors.New("cannot flush state file")
	}
	if replace {
		err = os.Rename(f.Name(), path)
	} else {
		err = os.Link(f.Name(), path)
	}
	if err != nil {
		return errors.New("cannot publish state file; destination may already exist")
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return errors.New("cannot open state directory")
	}
	defer dir.Close()
	if dir.Sync() != nil {
		return errors.New("cannot flush state directory")
	}
	return nil
}
func runState(args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	return stateCommand(ctx, args)
}
