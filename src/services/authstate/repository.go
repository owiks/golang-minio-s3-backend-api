package authstate

import (
	"context"
	"strconv"
	"time"
)

func formatVersion(v int64) string { return strconv.FormatInt(v, 10) }

type Repository interface {
	Bootstrap(context.Context, Connection, Access, Event) error
	Get(context.Context, string) (Access, Connection, error)
	Connections(context.Context, string) ([]Connection, error)
	Accesses(context.Context, string, string) ([]Access, error)
	Events(context.Context, string) ([]Event, error)
	Apply(context.Context, Command) (Result, error)
	Export(context.Context) (Backup, error)
	Import(context.Context, Backup) error
	Touch(context.Context, string, time.Time) error
	Reencrypt(context.Context, Connection) error
	Prune(context.Context, time.Time) (int, error)
	Ping(context.Context) error
	Close() error
}
type SecretBox interface {
	Seal([]byte, []byte) (Ciphertext, error)
	Open(Ciphertext, []byte) ([]byte, error)
	ActiveID() string
}
type Backup struct {
	Version     int              `json:"version"`
	Connections []ConnectionData `json:"connections"`
	Accesses    []AccessData     `json:"accesses"`
	Events      []Event          `json:"events"`
}
type Guard struct {
	Owner      string
	ActorID    string
	Revision   int64
	Generation int64
	Previous   bool
}
type Command struct {
	Guard              Guard
	Kind               string
	TargetID           string
	ConnectionID       string
	ConnectionRevision int64
	Revision           int64
	Access             Access
	Policy             *Policy
	Name               *string
	ExpiresAt          int64
	Digest             string
	GraceSeconds       int64
	Cipher             Ciphertext
	SecretVersion      int64
	Event              Event
}
type Result struct {
	Access     Access
	Connection Connection
}

// CredentialVerifier proves the replacement with the native upstream before
// the guarded command can replace stored credentials without changing grants.
type CredentialVerifier interface {
	Verify(context.Context, ConnectionView, Secrets) error
}
