// Package commandlog fences explicitly supported, non-idempotent API commands.
// It records a command before upstream I/O; an uncertain result is never a retry.
package commandlog

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
)

const (
	LeaseTTL         = time.Hour
	Retention        = 24 * time.Hour
	MaxRecords       = 16384
	MaxPerActor      = 128
	MaxResponseBytes = 64 << 10
	// JSON may escape every payload byte as a six-byte Unicode sequence.
	MaxEntryBytes    = 6*MaxResponseBytes + 8192
	MaxSnapshotBytes = 64 << 20

	Pending   = "pending"
	Completed = "completed"
	Unknown   = "unknown"
)

var (
	ErrInvalid     = errors.New("invalid command record")
	ErrUnavailable = errors.New("command log unavailable")
	ErrConflict    = errors.New("idempotency key reused with a different command")
	ErrInProgress  = errors.New("command is still in progress")
	ErrUnknown     = errors.New("command outcome is uncertain")
	ErrLimit       = errors.New("command log capacity reached")
	ErrNotFound    = errors.New("command record not found")
)

type Scope struct {
	ActorID      string `json:"actorID"`
	ConnectionID string `json:"connectionID"`
	Operation    string `json:"operation"`
}

// Response contains a neutral result from an explicitly supported command.
// Payload must be built from a trusted DTO; raw HTTP responses and secrets do
// not belong in the command log. MessageID is rendered only at the HTTP boundary.
type Response struct {
	Status    int             `json:"status"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Code      string          `json:"code,omitempty"`
	MessageID string          `json:"messageID,omitempty"`
}

type Entry struct {
	ID          string    `json:"id"`
	Scope       Scope     `json:"scope"`
	KeyHash     string    `json:"keyHash"`
	Fingerprint string    `json:"fingerprint"`
	CreatedAt   time.Time `json:"createdAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
	LeaseUntil  time.Time `json:"leaseUntil"`
	State       string    `json:"state"`
	Response    *Response `json:"response,omitempty"`
}

func (s Scope) Valid() bool {
	return boundedText(s.ActorID, 64, false) && boundedText(s.ConnectionID, 64, false) && boundedText(s.Operation, 256, false)
}

func boundedText(value string, maximum int, empty bool) bool {
	return (empty || value != "") && len(value) <= maximum && utf8.ValidString(value) && !strings.ContainsFunc(value, unicode.IsControl)
}

func validHash(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func ValidReservation(scope Scope, keyHash, fingerprint string, now time.Time) bool {
	return scope.Valid() && validHash(keyHash) && validHash(fingerprint) && !now.IsZero() && now.Year() >= 1970 && now.Year() < 9999
}

func (r Response) Valid() bool {
	return r.Status >= 200 && r.Status < 300 && validPayload(r.Payload) && boundedText(r.Code, 128, true) && boundedText(r.MessageID, 256, true)
}

func NewEntry(scope Scope, keyHash, fingerprint string, now time.Time) Entry {
	now = now.UTC().Truncate(time.Microsecond)
	return Entry{ID: securetoken.New(), Scope: scope, KeyHash: keyHash, Fingerprint: fingerprint, CreatedAt: now, ExpiresAt: now.Add(Retention), LeaseUntil: now.Add(LeaseTTL), State: Pending}
}

func (e Entry) Clone() Entry {
	if e.Response != nil {
		response := *e.Response
		response.Payload = append(json.RawMessage(nil), response.Payload...)
		e.Response = &response
	}
	return e
}

// Replay checks a reserved command without ever starting a second execution.
// The caller persists Unknown when a pending lease has expired.
func Replay(e Entry, fingerprint string, now time.Time) (Entry, error) {
	e = e.Clone()
	if e.Fingerprint != fingerprint {
		return e, ErrConflict
	}
	switch e.State {
	case Completed:
		return e, nil
	case Pending:
		if now.Before(e.LeaseUntil) {
			return e, ErrInProgress
		}
		e.State, e.Response = Unknown, nil
		return e, ErrUnknown
	default:
		return e, ErrUnknown
	}
}

func ValidEntry(e Entry) bool {
	if !boundedText(e.ID, 64, false) || !ValidReservation(e.Scope, e.KeyHash, e.Fingerprint, e.CreatedAt) || !e.ExpiresAt.Equal(e.CreatedAt.Add(Retention)) || !e.LeaseUntil.Equal(e.CreatedAt.Add(LeaseTTL)) {
		return false
	}
	switch e.State {
	case Pending, Unknown:
		return e.Response == nil
	case Completed:
		return e.Response != nil && e.Response.Valid()
	default:
		return false
	}
}

// Bound the neutral JSON result, independently of HTML escaping applied when an
// entry is stored or included in a backup. UseNumber retains integer precision.
func validPayload(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	if len(raw) > 6*MaxResponseBytes || !json.Valid(raw) {
		return false
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return false
	}
	var compact bytes.Buffer
	encoder := json.NewEncoder(&compact)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value) == nil && compact.Len()-1 <= MaxResponseBytes
}
