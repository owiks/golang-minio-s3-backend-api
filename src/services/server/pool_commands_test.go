package server

import (
	"context"
	"minio-web/api/src/services/apperror"
	"testing"
)

func TestPoolCommandsRejectMissingTypedNilAndUnknownOperationsBeforeIO(t *testing.T) {
	for _, command := range []PoolCommand{nil, (*StartRebalanceCommand)(nil), (*DecommissionPoolCommand)(nil)} {
		if err := ChangePools(context.Background(), nil, command); err == nil || apperror.Public(err).Code != "InvalidPoolOperation" {
			t.Fatal("invalid command reached client", err)
		}
	}
	if command, err := (PoolChangeRequest{Operation: "unknown"}).Command(); err == nil || command != nil {
		t.Fatal("unknown operation accepted")
	}
	for _, operation := range []string{"start", "stop", "decommission", "cancel"} {
		command, err := (PoolChangeRequest{Operation: operation, Pool: 3, Revision: "checked", Confirmation: "confirmed"}).Command()
		if err != nil {
			t.Fatal(err)
		}
		got, pool, revision, confirmation := command.poolChange()
		if got != operation || revision != "checked" || confirmation != "confirmed" || (operation == "decommission" || operation == "cancel") && pool != 3 {
			t.Fatal("command lost scope")
		}
	}
}
