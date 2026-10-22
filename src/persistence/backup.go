package persistence

import (
	"context"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/commandlog"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/operations"
)

// Backup retains the original auth-state JSON fields. Legacy files without
// command or operation snapshots remain valid for restore, which revokes all
// restored credentials.
type Backup struct {
	authstate.Backup
	Commands   []commandlog.Entry    `json:"commands"`
	Operations []operations.Snapshot `json:"operations"`
}

// ExportBackup includes command fences as well as credentials. Preserving
// access on transfer requires the caller to stop every source and target writer.
func ExportBackup(ctx context.Context, cfg config.Config, manager *authstate.Manager) (Backup, error) {
	if cfg.State.Driver == "" || cfg.State.Driver == "memory" {
		return Backup{}, authstate.Invalid()
	}
	commands, err := OpenCommands(ctx, cfg)
	if err != nil {
		return Backup{}, err
	}
	defer commands.Close()
	entries, err := commands.Export(ctx)
	if err != nil {
		return Backup{}, err
	}
	jobs, err := OpenOperations(ctx, cfg)
	if err != nil {
		return Backup{}, err
	}
	defer jobs.Close()
	records, err := jobs.Export(ctx)
	if err != nil {
		return Backup{}, err
	}
	access, err := manager.Export(ctx)
	if err != nil {
		return Backup{}, err
	}
	return Backup{Backup: access, Commands: entries, Operations: records}, nil
}

// RestoreBackup publishes execution fences before access records. These use
// separate transactions: a failure can leave fences without any usable access,
// never usable access without its fences. Repeating the same offline restore
// resumes identical imported fences; conflicting state is never overwritten.
func RestoreBackup(ctx context.Context, cfg config.Config, manager *authstate.Manager, backup Backup, preserve bool) error {
	if cfg.State.Driver == "" || cfg.State.Driver == "memory" {
		return authstate.Invalid()
	}
	if err := authstate.ValidateBackup(backup.Backup); err != nil {
		return err
	}
	if preserve && backup.Commands == nil {
		return commandlog.ErrInvalid
	}
	if preserve && backup.Operations == nil {
		return operations.ErrInvalid
	}
	if !operations.ValidBackup(backup.Operations) {
		return operations.ErrInvalid
	}
	entries := make([]commandlog.Entry, len(backup.Commands))
	for i, entry := range backup.Commands {
		entries[i] = entry.Clone()
		// No worker from the old process can finish in the destination database.
		if entries[i].State == commandlog.Pending {
			entries[i].State = commandlog.Unknown
		}
	}
	if err := commandlog.ValidateEntries(entries); err != nil {
		return err
	}
	existing, err := manager.Export(ctx)
	if err != nil {
		return err
	}
	if len(existing.Connections)+len(existing.Accesses)+len(existing.Events) != 0 {
		return authstate.Conflict()
	}
	commands, err := OpenCommands(ctx, cfg)
	if err != nil {
		return err
	}
	defer commands.Close()
	if err = commands.Import(ctx, entries); err != nil {
		return err
	}
	jobs, err := OpenOperations(ctx, cfg)
	if err != nil {
		return err
	}
	defer jobs.Close()
	if err = jobs.Import(ctx, backup.Operations); err != nil {
		return err
	}
	return manager.Restore(ctx, backup.Backup, preserve)
}
