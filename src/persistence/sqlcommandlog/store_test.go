package sqlcommandlog_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/sqlcommandlog"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/commandlog"
	"github.com/owiks/golang-minio-s3-backend-api/tests/commandcontract"
	"github.com/owiks/golang-minio-s3-backend-api/tests/databasefixtures"
	"github.com/owiks/golang-minio-s3-backend-api/tests/statefixtures"
)

func open(t *testing.T, cfg config.State) *sqlcommandlog.Store {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	repository, err := sqlcommandlog.Open(ctx, sqlcommandlog.Config{Database: cfg.Database})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	return repository
}

func TestSQLiteContract(t *testing.T) {
	cfg := databasefixtures.Database(t, "sqlite")
	statefixtures.Migrate(t, cfg)
	commandcontract.Run(t, open(t, cfg))
}

func TestSQLNativeContract(t *testing.T) {
	if os.Getenv("STATE_INTEGRATION") != "1" {
		t.Skip("requires disposable PostgreSQL/MySQL fixtures")
	}
	for _, driver := range []string{"postgres", "mysql"} {
		t.Run(driver, func(t *testing.T) {
			cfg := databasefixtures.Database(t, driver)
			statefixtures.Migrate(t, cfg)
			commandcontract.Run(t, open(t, cfg))
		})
	}
}

func TestSQLiteRestartDoesNotRepeatCommand(t *testing.T) {
	cfg := databasefixtures.Database(t, "sqlite")
	statefixtures.Migrate(t, cfg)
	before := open(t, cfg)
	now := time.Now().UTC().Truncate(time.Microsecond)
	scope := commandlog.Scope{ActorID: "actor", ConnectionID: "connection", Operation: "POST objects/transfer"}
	sum := sha256.Sum256([]byte("command"))
	hash := hex.EncodeToString(sum[:])
	entry, _, err := before.Reserve(context.Background(), scope, hash, hash, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = before.Close(); err != nil {
		t.Fatal(err)
	}
	after := open(t, cfg)
	replay, fresh, err := after.Reserve(context.Background(), scope, hash, hash, now.Add(commandlog.LeaseTTL))
	if fresh || !errors.Is(err, commandlog.ErrUnknown) || replay.ID != entry.ID || replay.State != commandlog.Unknown {
		t.Fatal("restart granted another execution", fresh, replay.State, err)
	}
}

func TestSQLiteMultipleConnectionsReserveOnce(t *testing.T) {
	cfg := databasefixtures.Database(t, "sqlite")
	statefixtures.Migrate(t, cfg)
	stores := []*sqlcommandlog.Store{open(t, cfg), open(t, cfg)}
	now := time.Now().UTC().Truncate(time.Microsecond)
	scope := commandlog.Scope{ActorID: "actor", ConnectionID: "connection", Operation: "POST objects/transfer"}
	sum := sha256.Sum256([]byte("shared command"))
	hash := hex.EncodeToString(sum[:])
	var freshCount atomic.Int64
	var wg sync.WaitGroup
	for index := range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, fresh, err := stores[index%len(stores)].Reserve(context.Background(), scope, hash, hash, now)
			if fresh && err == nil {
				freshCount.Add(1)
			} else if fresh || !errors.Is(err, commandlog.ErrInProgress) {
				t.Error("multiple-connection reservation", fresh, err)
			}
		}()
	}
	wg.Wait()
	if freshCount.Load() != 1 {
		t.Fatal("multiple connections claimed command", freshCount.Load())
	}
}

func TestBackupContract(t *testing.T) {
	drivers := []string{"sqlite"}
	if os.Getenv("STATE_INTEGRATION") == "1" {
		drivers = append(drivers, "postgres", "mysql")
	}
	for _, driver := range drivers {
		t.Run(driver, func(t *testing.T) {
			sourceCfg, targetCfg := databasefixtures.Database(t, driver), databasefixtures.Database(t, driver)
			statefixtures.Migrate(t, sourceCfg)
			statefixtures.Migrate(t, targetCfg)
			commandcontract.Backup(t, open(t, sourceCfg), open(t, targetCfg))
		})
	}
}
