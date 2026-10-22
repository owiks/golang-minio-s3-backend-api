package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestRootStartsAPIOnlyWithoutArguments(t *testing.T) {
	want := errors.New("server stopped")
	started := 0
	command := newRootCommand(func() error { started++; return want }, io.Discard, io.Discard)
	command.SetArgs([]string{})
	if err := command.Execute(); !errors.Is(err, want) || started != 1 {
		t.Fatalf("start = %d, error = %v", started, err)
	}
}

func TestCommandHelpDoesNotOpenStateOrStartAPI(t *testing.T) {
	t.Setenv("API_DB_DRIVER", "invalid")
	for _, args := range [][]string{
		{"--help"}, {"state", "--help"}, {"migrate", "--help"},
		{"migrate", "up", "--help"}, {"migrate", "down", "--help"},
		{"migrate", "status", "--help"}, {"migrate", "version", "--help"},
		{"state", "keygen", "--help"}, {"state", "key-add", "--help"},
		{"state", "backup", "--help"}, {"state", "restore", "--help"},
		{"state", "transfer", "--help"}, {"state", "verify", "--help"},
		{"state", "reencrypt", "--help"}, {"state", "prune", "--help"},
		{"state", "migrate", "--help"}, {"help", "state", "restore"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var output bytes.Buffer
			command := newRootCommand(func() error { t.Fatal("help started API"); return nil }, &output, io.Discard)
			command.SetArgs(args)
			if err := command.Execute(); err != nil || !strings.Contains(output.String(), "Usage:") {
				t.Fatalf("help = %q, error = %v", output.String(), err)
			}
		})
	}
}

func TestCommandValidationRejectsUnknownArgumentsAndUnrelatedFlags(t *testing.T) {
	t.Setenv("API_DB_DRIVER", "invalid")
	outputFile := filepath.Join(t.TempDir(), "keyring.json")
	for _, tc := range []struct {
		args []string
		want error
	}{
		{[]string{"not-a-command"}, errMigrationCommand},
		{[]string{"--unknown=private-value"}, errMigrationCommand},
		{[]string{"migrate"}, errMigrationCommand},
		{[]string{"migrate", "unknown"}, errMigrationCommand},
		{[]string{"migrate", "version", "extra"}, errMigrationCommand},
		{[]string{"migrate", "up", "--offline=invalid"}, errMigrationCommand},
		{[]string{"state"}, errStateCommand},
		{[]string{"state", "unknown"}, errStateCommand},
		{[]string{"state", "keygen", "--out", outputFile, "extra"}, errStateCommand},
		{[]string{"state", "keygen", "--out", outputFile, "--offline"}, errStateCommand},
		{[]string{"state", "verify", "--before=private-value"}, errStateCommand},
		{[]string{"state", "transfer", "--offline=invalid"}, errStateCommand},
		{[]string{"state", "migrate", "up"}, errMigrationCommand},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var output bytes.Buffer
			command := newRootCommand(func() error { t.Fatal("invalid command started API"); return nil }, &output, &output)
			command.SetArgs(tc.args)
			if err := command.Execute(); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if output.Len() != 0 {
				t.Fatalf("invalid command printed %q", output.String())
			}
		})
	}
	if _, err := os.Stat(outputFile); !os.IsNotExist(err) {
		t.Fatalf("invalid keygen created a keyring: %v", err)
	}
}

func TestMaintenanceAssertionsPrecedeDatabaseConfiguration(t *testing.T) {
	t.Setenv("API_DB_DRIVER", "invalid")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"migrate", "down"}, "migrate down requires --offline with API writers stopped"},
		{[]string{"migrate", "down", "--offline=false"}, "migrate down requires --offline with API writers stopped"},
		{[]string{"state", "restore", "--in=backup.json"}, "restore requires --in and --offline with API writers stopped"},
		{[]string{"state", "transfer"}, "transfer requires --offline and API_TARGET_DB_* connection fields"},
		{[]string{"state", "prune", "--before=invalid"}, errStateCommand.Error()},
	} {
		command := newRootCommand(func() error { t.Fatal("maintenance started API"); return nil }, io.Discard, io.Discard)
		command.SetArgs(tc.args)
		if err := command.Execute(); err == nil || err.Error() != tc.want {
			t.Fatalf("%v: error = %v, want %q", tc.args, err, tc.want)
		}
	}
}

func TestMigrationAliasAndIndependentFlagValues(t *testing.T) {
	t.Setenv("API_DB_DRIVER", "memory")
	t.Setenv("API_TARGET_DB_DRIVER", "invalid")
	for _, args := range [][]string{{"state", "migrate"}, {"migrate", "version"}} {
		var output bytes.Buffer
		command := newRootCommand(func() error { t.Fatal("migration started API"); return nil }, &output, io.Discard)
		command.SetArgs(args)
		if err := command.Execute(); err != nil {
			t.Fatal(args, err)
		}
		if args[0] == "migrate" && output.String() != "0\n" {
			t.Fatalf("version output = %q", output.String())
		}
	}
	command := newRootCommand(func() error { return nil }, io.Discard, io.Discard)
	command.SetArgs([]string{"migrate", "version", "--target"})
	if err := command.Execute(); err == nil {
		t.Fatal("target configuration was ignored")
	}
	// A new invocation must not retain --target from an earlier command tree.
	if err := migrateCommand(context.Background(), []string{"version"}, io.Discard); err != nil {
		t.Fatal("flag value leaked between command trees", err)
	}
}

func TestStateProgressUsesCommandOutput(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	keyring := filepath.Join(dir, "keyring.json")
	if err := stateCommand(ctx, []string{"keygen", "--out", keyring}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("API_DB_DRIVER", "sqlite")
	t.Setenv("API_DB_SQLITE_PATH", filepath.Join(dir, "state.db"))
	t.Setenv("API_STATE_KEYRING_FILE", keyring)
	if err := migrateCommand(ctx, []string{"up"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"state", "reencrypt"}, "Reencrypted connections: 0\n"},
		{[]string{"state", "prune", "--before=2026-01-01T00:00:00Z"}, "Pruned records: 0\n"},
	} {
		var output bytes.Buffer
		command := newRootCommand(func() error { return nil }, &output, io.Discard)
		command.SetArgs(tc.args)
		if err := command.Execute(); err != nil || output.String() != tc.want {
			t.Fatalf("%v: output = %q, error = %v", tc.args, output.String(), err)
		}
	}
}

func TestMaintenanceContextKeepsCancellationAndDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	command := &cobra.Command{}
	command.SetContext(ctx)
	command.SetOut(io.Discard)
	err := maintenanceRun(func(ctx context.Context, output io.Writer) error {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 5*time.Minute {
			t.Fatal("maintenance deadline is missing")
		}
		if output != io.Discard {
			t.Fatal("output writer was not passed through")
		}
		return ctx.Err()
	})(command, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("maintenance lost parent cancellation", err)
	}
}
