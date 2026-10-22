package commandlog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestGlobalLimitDoesNotDiscardUnknownCommands(t *testing.T) {
	repository := NewMemory()
	t.Cleanup(func() { _ = repository.Close() })
	now := time.Now().UTC().Truncate(time.Microsecond)
	hash := strings.Repeat("a", 64)
	for index := range MaxRecords {
		scope := Scope{ActorID: fmt.Sprintf("actor-%d", index), ConnectionID: "connection", Operation: "transfer"}
		entry := NewEntry(scope, hash, hash, now)
		entry.State = Unknown
		repository.entries[memoryKey{scope: scope, hash: hash}] = entry
	}
	scope := Scope{ActorID: "new actor", ConnectionID: "connection", Operation: "transfer"}
	if _, _, err := repository.Reserve(context.Background(), scope, hash, hash, now); !errors.Is(err, ErrLimit) {
		t.Fatal("global command limit ignored", err)
	}
	if len(repository.entries) != MaxRecords {
		t.Fatal("capacity evicted unexpired command fences")
	}
}

func TestEntryValidationRejectsCorruptResults(t *testing.T) {
	hash := strings.Repeat("a", 64)
	entry := NewEntry(Scope{ActorID: "actor", ConnectionID: "connection", Operation: "transfer"}, hash, hash, time.Now())
	entry.State = Completed
	if ValidEntry(entry) {
		t.Fatal("completed command without a result accepted")
	}
	entry.Response = &Response{Status: 200}
	if !ValidEntry(entry) {
		t.Fatal("valid completed command rejected")
	}
	entry.ExpiresAt = entry.ExpiresAt.Add(time.Hour)
	if ValidEntry(entry) {
		t.Fatal("corrupt retention accepted")
	}
}
