// Package operations owns resumable operation records and execution fences.
// A transfer whose outcome is uncertain is never automatically executed again.
package operations

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/transfers"
)

const (
	MaxRecords       = 1024
	MaxPerActor      = 32
	MaxTransfers     = 1000
	MaxRecordBytes   = 1 << 20
	MaxSnapshotBytes = 64 << 20
	MaxScanPages     = 8192
	Retention        = 24 * time.Hour
	Queued           = "queued"
	Running          = "running"
	Completed        = "completed"
	Failed           = "failed"
	Cancelled        = "cancelled"
	Unknown          = "unknown"
)

var (
	ErrInvalid     = errors.New("invalid operation")
	ErrNotFound    = errors.New("operation not found")
	ErrConflict    = errors.New("operation revision or lease changed")
	ErrLimit       = errors.New("operation capacity reached")
	ErrUnavailable = errors.New("operation storage unavailable")
)

type Scope struct {
	ActorID      string `json:"actorID"`
	ConnectionID string `json:"connectionID"`
}
type ScanSpec struct {
	Bucket string `json:"bucket"`
	Prefix string `json:"prefix"`
}
type Spec struct {
	Transfers []transfers.FileTransferRequest `json:"transfers,omitempty"`
	Scan      *ScanSpec                       `json:"scan,omitempty"`
}
type ItemResult struct {
	Index  int    `json:"index"`
	State  string `json:"state"`
	Copied bool   `json:"copied"`
	Moved  bool   `json:"moved"`
	Code   string `json:"code,omitempty"`
}
type ScanProgress struct {
	Objects int64    `json:"objects"`
	Bytes   int64    `json:"bytes"`
	Pages   int      `json:"pages"`
	Token   string   `json:"token,omitempty"`
	Seen    []string `json:"seen,omitempty"`
}
type Snapshot struct {
	ID              string       `json:"id"`
	Scope           Scope        `json:"scope"`
	Spec            Spec         `json:"spec"`
	Kind            string       `json:"kind"`
	State           string       `json:"state"`
	Code            string       `json:"code,omitempty"`
	Revision        uint64       `json:"revision"`
	CreatedAt       time.Time    `json:"createdAt"`
	UpdatedAt       time.Time    `json:"updatedAt"`
	ExpiresAt       time.Time    `json:"expiresAt,omitempty"`
	Deadline        time.Time    `json:"deadline"`
	NextIndex       int          `json:"nextIndex"`
	Items           []ItemResult `json:"items,omitempty"`
	Scan            ScanProgress `json:"progress"`
	CancelRequested bool         `json:"cancelRequested"`
	InFlight        bool         `json:"inFlight"`
	WorkerID        string       `json:"workerID,omitempty"`
	LeaseID         string       `json:"leaseID,omitempty"`
	LeaseUntil      time.Time    `json:"leaseUntil,omitempty"`
	LastServedAt    time.Time    `json:"lastServedAt"`
	// Timing is absent in records written before execution accounting existed.
	// Keeping it optional avoids inventing queue or execution time on restore.
	Timing *Timing `json:"timing,omitempty"`
	// NotBefore also fences other queued work using this connection. It is kept
	// after cancellation so deleting a job cannot immediately bypass backpressure.
	NotBefore           time.Time `json:"notBefore,omitempty"`
	ThrottleConsecutive int       `json:"throttleConsecutive,omitempty"`
	ThrottleTotal       int       `json:"throttleTotal,omitempty"`
}

func validID(s string) bool {
	return s != "" && len(s) <= 64 && utf8.ValidString(s) && !strings.ContainsAny(s, "\x00\r\n")
}
func (s Scope) Valid() bool { return validID(s.ActorID) && validID(s.ConnectionID) }
func (s Spec) Valid() bool {
	if (len(s.Transfers) == 0) == (s.Scan == nil) || len(s.Transfers) > MaxTransfers {
		return false
	}
	validKey := func(s string) bool { return len(s) <= 1024 && utf8.ValidString(s) && !strings.ContainsRune(s, 0) }
	validBucket := func(s string) bool { return s != "" && len(s) <= 63 && !strings.ContainsAny(s, "\x00\r\n") }
	if s.Scan != nil {
		return validBucket(s.Scan.Bucket) && validKey(s.Scan.Prefix)
	}
	for _, r := range s.Transfers {
		if !validBucket(r.SourceBucket) || !validBucket(r.DestinationBucket) || !validKey(r.SourceKey) || !validKey(r.DestinationKey) || !validKey(r.SourcePrefix) || !validKey(r.DestinationPrefix) || r.SourceKey == "" || r.DestinationKey == "" || r.SourceRevision == "" || r.DestinationRevision == "" || len(r.SourceRevision) > 256 || len(r.DestinationRevision) > 256 || !transfers.ValidTransferMapping(r.SourceKey, r.DestinationKey, r.SourcePrefix, r.DestinationPrefix, r.SourceBucket == r.DestinationBucket) || r.SourceBucket == r.DestinationBucket && r.SourceKey == r.DestinationKey {
			return false
		}
	}
	raw, err := json.Marshal(s)
	// Leave room for bounded per-item results and scan checkpoints.
	return err == nil && len(raw) <= MaxRecordBytes/2
}
func (s Spec) Clone() Spec {
	s.Transfers = append([]transfers.FileTransferRequest(nil), s.Transfers...)
	if s.Scan != nil {
		v := *s.Scan
		s.Scan = &v
	}
	return s
}
func (s Snapshot) Clone() Snapshot {
	s.Spec = s.Spec.Clone()
	s.Items = append([]ItemResult(nil), s.Items...)
	s.Scan.Seen = append([]string(nil), s.Scan.Seen...)
	if s.Timing != nil {
		timing := *s.Timing
		s.Timing = &timing
	}
	return s
}

// Same compares complete offline records; it is never a substitute for revision
// and lease checks in live execution.
func Same(a, b Snapshot) bool {
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	return err == nil && bytes.Equal(left, right)
}
func (s Snapshot) Terminal() bool {
	return s.State == Completed || s.State == Failed || s.State == Cancelled || s.State == Unknown
}
func (s Snapshot) Retryable() bool       { return s.State == Failed && !s.InFlight }
func normalized(now time.Time) time.Time { return now.UTC().Truncate(time.Microsecond) }

// A clock adjustment or a different replica must not move a durable transition
// behind a previously acknowledged timestamp or discard a confirmed outcome.
func recordTime(s Snapshot, now time.Time) time.Time {
	now = normalized(now)
	if s.UpdatedAt.After(now) {
		return s.UpdatedAt
	}
	return now
}
func New(scope Scope, spec Spec, deadline, now time.Time) (Snapshot, error) {
	now = normalized(now)
	deadline = normalized(deadline)
	if !scope.Valid() || !spec.Valid() || now.IsZero() || !deadline.After(now) || deadline.Year() > 9998 {
		return Snapshot{}, ErrInvalid
	}
	kind := "transfer"
	if spec.Scan != nil {
		kind = "scan"
	}
	return Snapshot{ID: securetoken.New(), Scope: scope, Spec: spec.Clone(), Kind: kind, State: Queued, Revision: 1, CreatedAt: now, UpdatedAt: now, Deadline: deadline, Timing: &Timing{WaitingSince: now}}, nil
}
func Valid(s Snapshot) bool {
	if !validID(s.ID) || !s.Scope.Valid() || !s.Spec.Valid() || s.Revision == 0 || s.CreatedAt.IsZero() || s.UpdatedAt.Before(s.CreatedAt) || !s.Deadline.After(s.CreatedAt) || len(s.Items) > MaxTransfers || s.NextIndex < 0 || s.NextIndex > len(s.Spec.Transfers) || len(s.Scan.Token) > 16384 || s.Scan.Pages < 0 || s.Scan.Pages > MaxScanPages || len(s.Scan.Seen) > MaxScanPages || s.Scan.Objects < 0 || s.Scan.Bytes < 0 || !safeCode(s.Code) {
		return false
	}
	if s.Kind != "transfer" && s.Kind != "scan" || (s.Kind == "scan") != (s.Spec.Scan != nil) {
		return false
	}
	if s.State != Queued && s.State != Running && !s.Terminal() {
		return false
	}
	if s.State == Running && (!validID(s.WorkerID) || !validID(s.LeaseID) || s.LeaseUntil.IsZero()) {
		return false
	}
	if s.State != Running && (s.InFlight || s.WorkerID != "" || s.LeaseID != "" || !s.LeaseUntil.IsZero()) {
		return false
	}
	if !validTiming(s) || !validBackpressure(s) {
		return false
	}
	if s.Terminal() && !s.ExpiresAt.Equal(s.UpdatedAt.Add(Retention)) || !s.Terminal() && !s.ExpiresAt.IsZero() {
		return false
	}
	if s.Kind == "transfer" {
		expected := s.NextIndex
		if s.State == Unknown {
			expected++
		}
		if len(s.Items) != expected || s.State == Completed && s.NextIndex != len(s.Spec.Transfers) || s.Scan.Objects != 0 || s.Scan.Bytes != 0 || s.Scan.Pages != 0 || s.Scan.Token != "" || len(s.Scan.Seen) != 0 {
			return false
		}
		if s.InFlight && s.NextIndex >= len(s.Spec.Transfers) {
			return false
		}
	} else if s.NextIndex != 0 || len(s.Items) != 0 || s.InFlight || s.State == Unknown || s.State == Completed && s.Scan.Token != "" || s.Scan.Pages < len(s.Scan.Seen) {
		return false
	}
	for position, i := range s.Items {
		if i.Index != position || i.Index >= len(s.Spec.Transfers) || !safeCode(i.Code) || i.State != "success" && i.State != "partial" && i.State != Unknown || (i.State == Unknown) != (s.State == Unknown && position == s.NextIndex) || i.Moved && (!i.Copied || !s.Spec.Transfers[i.Index].Move) || i.State == "success" && !i.Copied {
			return false
		}
	}
	seen := map[string]bool{}
	for _, h := range s.Scan.Seen {
		if len(h) != 64 || seen[h] {
			return false
		}
		seen[h] = true
		if _, err := hex.DecodeString(h); err != nil {
			return false
		}
	}
	if s.Kind == "scan" && (s.Scan.Token != "" && (len(s.Scan.Seen) == 0 || s.Scan.Seen[len(s.Scan.Seen)-1] != tokenHash(s.Scan.Token)) || s.Scan.Pages == 0 && (s.Scan.Token != "" || len(s.Scan.Seen) != 0)) {
		return false
	}
	raw, err := json.Marshal(s)
	return err == nil && len(raw) <= MaxRecordBytes
}
func safeCode(s string) bool {
	if len(s) > 128 {
		return false
	}
	for _, r := range s {
		if r != '_' && r != '-' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}
func tokenHash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func terminal(s *Snapshot, state, code string, now time.Time) {
	now = recordTime(*s, now)
	finishTiming(s, now)
	s.State = state
	s.Code = code
	s.InFlight = false
	s.WorkerID = ""
	s.LeaseID = ""
	s.LeaseUntil = time.Time{}
	s.UpdatedAt = normalized(now)
	s.ExpiresAt = s.UpdatedAt.Add(Retention)
	s.Revision++
}
