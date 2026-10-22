package commandlog_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/commandlog"
	"github.com/owiks/golang-minio-s3-backend-api/tests/commandcontract"
)

func TestMemoryContract(t *testing.T) {
	repository := commandlog.NewMemory()
	t.Cleanup(func() { _ = repository.Close() })
	commandcontract.Run(t, repository)
}

func TestClosedAndCancelled(t *testing.T) {
	repository := commandlog.NewMemory()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(repository.Ping(ctx), commandlog.ErrUnavailable) {
		t.Fatal("canceled call accepted")
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(repository.Ping(context.Background()), commandlog.ErrUnavailable) {
		t.Fatal("closed repository accepted call")
	}
	scope := commandlog.Scope{ActorID: "actor", ConnectionID: "connection", Operation: "operation"}
	const hash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, _, err := repository.Reserve(context.Background(), scope, hash, hash, time.Now()); !errors.Is(err, commandlog.ErrUnavailable) {
		t.Fatal("closed repository accepted reservation", err)
	}
}

func TestMemoryBackup(t *testing.T) {
	source, target := commandlog.NewMemory(), commandlog.NewMemory()
	t.Cleanup(func() { _ = source.Close(); _ = target.Close() })
	commandcontract.Backup(t, source, target)
}
