package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/secretbox"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
)

var errStateCommand = errors.New("invalid state command; use: state keygen|key-add|migrate|verify|backup|restore|transfer|reencrypt|prune")

type stateOptions struct {
	before        string
	out           string
	in            string
	keyfile       string
	offline       bool
	targetKeyring string
}

func newStateCommand() *cobra.Command {
	command := &cobra.Command{
		Use: "state", Short: "Maintain connections, access records and encryption keys",
		SilenceUsage: true, SilenceErrors: true,
		Args: noCommandArguments(errStateCommand),
		RunE: func(_ *cobra.Command, _ []string) error { return errStateCommand },
	}
	command.SetFlagErrorFunc(func(_ *cobra.Command, _ error) error { return errStateCommand })
	for _, action := range []struct{ name, description string }{
		{"keygen", "Create a new private keyring file without overwriting existing files"},
		{"key-add", "Add and activate a key while retaining existing decryption keys"},
		{"verify", "Verify stored connections and access records"},
		{"backup", "Export persistent state to a new private backup file"},
		{"restore", "Restore a backup into an empty database and revoke restored access"},
		{"transfer", "Transfer state to API_TARGET_DB_* while preserving access"},
		{"reencrypt", "Reencrypt connections with the active key"},
		{"prune", "Remove expired access records before an absolute retention boundary"},
	} {
		command.AddCommand(newStateAction(action.name, action.description))
	}
	// Compatibility alias: migration remains explicit and never runs as part of Open.
	alias := newMigrationAction("up", "Apply migrations; compatibility alias for migrate up")
	alias.Use = "migrate"
	command.AddCommand(alias)
	return command
}

func newStateAction(name, description string) *cobra.Command {
	options := stateOptions{}
	command := &cobra.Command{
		Use: name, Short: description,
		Args: noCommandArguments(errStateCommand),
		RunE: maintenanceRun(func(ctx context.Context, output io.Writer) error {
			return executeState(ctx, name, options, output)
		}),
	}
	switch name {
	case "keygen", "backup":
		command.Flags().StringVar(&options.out, "out", "", "New output file (required)")
	case "key-add":
		command.Flags().StringVar(&options.keyfile, "file", "", "Existing keyring file (required)")
	case "restore":
		command.Flags().StringVar(&options.in, "in", "", "Input backup file (required)")
		command.Flags().BoolVar(&options.offline, "offline", false, "Confirm all API writers on every host are stopped")
	case "transfer":
		command.Flags().BoolVar(&options.offline, "offline", false, "Confirm all API writers on every host are stopped")
		command.Flags().StringVar(&options.targetKeyring, "target-keyring-file", "", "Destination keyring file")
	case "prune":
		command.Flags().StringVar(&options.before, "before", "", "Absolute expiry retention boundary in RFC3339 (required)")
	}
	return command
}

func stateCommand(ctx context.Context, args []string) error {
	command := newStateCommand()
	command.SetArgs(append([]string{}, args...))
	command.SetOut(os.Stdout)
	command.SetErr(io.Discard)
	return command.ExecuteContext(ctx)
}

func executeState(ctx context.Context, name string, options stateOptions, output io.Writer) error {
	// Validate destructive command assertions before opening persistent state.
	switch name {
	case "keygen", "backup":
		if options.out == "" {
			return errStateCommand
		}
	case "key-add":
		if options.keyfile == "" {
			return errStateCommand
		}
	case "restore":
		if options.in == "" || !options.offline {
			return errors.New("restore requires --in and --offline with API writers stopped")
		}
	case "transfer":
		if !options.offline {
			return errors.New("transfer requires --offline and API_TARGET_DB_* connection fields")
		}
	case "prune":
		if _, err := time.Parse(time.RFC3339, options.before); err != nil {
			return errStateCommand
		}
	}
	if name == "keygen" || name == "key-add" {
		ring, err := secretbox.Generate()
		if err != nil {
			return err
		}
		if name == "keygen" {
			return writeJSONFile(options.out, ring, false)
		}
		data, err := readBounded(options.keyfile, 65536)
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
		return writeJSONFile(options.keyfile, existing, true)
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
		b, err := persistence.ExportBackup(ctx, cfg, m)
		if err != nil {
			return err
		}
		return writeJSONFile(options.out, b, false)
	case "restore":
		raw, err := readBounded(options.in, 64*1024*1024)
		if err != nil {
			return err
		}
		defer clear(raw)
		var b persistence.Backup
		if json.Unmarshal(raw, &b) != nil {
			return errStateCommand
		}
		return persistence.RestoreBackup(ctx, cfg, m, b, false)
	case "transfer":
		database, err := config.LoadTransferDatabase()
		if err != nil {
			return err
		}
		if database.Driver == "memory" {
			return errors.New("transfer requires a persistent API_TARGET_DB_DRIVER")
		}
		b, err := persistence.ExportBackup(ctx, cfg, m)
		if err != nil {
			return err
		}
		target := cfg
		target.State.Database = database
		if options.targetKeyring != "" {
			target.State.KeyringFile = options.targetKeyring
		}
		dest, err := persistence.Open(ctx, target)
		if err != nil {
			return err
		}
		defer dest.Close()
		return persistence.RestoreBackup(ctx, target, dest, b, true)
	case "prune":
		boundary, err := time.Parse(time.RFC3339, options.before)
		if err != nil {
			return errStateCommand
		}
		n, err := m.Prune(ctx, boundary)
		if err == nil {
			_, err = fmt.Fprintf(output, "Pruned records: %d\n", n)
		}
		return err
	case "reencrypt":
		n, err := m.Reencrypt(ctx)
		if err == nil {
			_, err = fmt.Fprintf(output, "Reencrypted connections: %d\n", n)
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
	f, err := os.CreateTemp(filepath.Dir(path), ".api-state-*")
	if err != nil {
		return errors.New("cannot create private state file")
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if f.Chmod(0600) != nil {
		return errors.New("cannot secure state file")
	}
	writer := &stateFileWriter{Writer: f, remaining: 64 * 1024 * 1024}
	if snapshot, ok := value.(interface{ WriteJSON(io.Writer) error }); ok {
		err = snapshot.WriteJSON(writer)
	} else {
		// Keyrings are bounded by the 64 KiB input limit. Keep their temporary
		// plaintext encoding explicitly erasable rather than in an encoder buffer.
		var raw []byte
		raw, err = json.MarshalIndent(value, "", "  ")
		defer clear(raw)
		if err == nil {
			_, err = writer.Write(append(raw, '\n'))
		}
	}
	if err != nil {
		return errors.New("cannot encode state file within 64 MiB")
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

// Reject the next chunk before crossing the output budget. The private temporary
// file is discarded on failure and can never replace the original keyring.
type stateFileWriter struct {
	io.Writer
	remaining int
}

func (w *stateFileWriter) Write(p []byte) (int, error) {
	if len(p) > w.remaining {
		return 0, errors.New("state file size limit")
	}
	n, err := w.Writer.Write(p)
	w.remaining -= n
	return n, err
}
