package operations

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
)

// Repository implementations serialize each transition, including admission and
// expired-lease recovery, across all workers sharing the same store.
type Repository interface {
	Insert(context.Context, Scope, Spec, time.Time, time.Time) (Snapshot, error)
	Get(context.Context, Scope, string) (Snapshot, error)
	List(context.Context, Scope) ([]Snapshot, error)
	Claim(context.Context, string, time.Time, time.Duration) (Snapshot, error)
	Save(context.Context, string, string, uint64, Checkpoint, time.Time) (Snapshot, error)
	Renew(context.Context, string, string, time.Time, time.Duration) error
	Cancel(context.Context, Scope, string, uint64, time.Time) (Snapshot, error)
	Retry(context.Context, Scope, string, uint64, time.Time) (Snapshot, error)
	Prune(context.Context, time.Time) error
	Ping(context.Context) error
	Close() error
	Export(context.Context) ([]Snapshot, error)
	Import(context.Context, []Snapshot) error
}

type TransferOutcome struct {
	Copied    bool
	Moved     bool
	Code      string
	Uncertain bool
}
type ScanPage struct {
	Objects   int64
	Bytes     int64
	NextToken string
}

// Checkpoint is a single fenced transition: start a transfer before upstream
// I/O, store its outcome, or atomically add a read-only scan page and cursor.
type Checkpoint struct {
	Yield    bool
	Begin    bool
	Transfer *TransferOutcome
	Page     *ScanPage
	FailCode string
}

func InsertRecord(records map[string]Snapshot, scope Scope, spec Spec, deadline, now time.Time) (Snapshot, error) {
	s, err := New(scope, spec, deadline, now)
	if err != nil {
		return Snapshot{}, err
	}
	PruneRecords(records, now)
	actor := 0
	for _, s := range records {
		if s.Scope.ActorID == scope.ActorID {
			actor++
		}
	}
	for actor >= MaxPerActor {
		if !evictTerminal(records, scope.ActorID) {
			return Snapshot{}, ErrLimit
		}
		actor--
	}
	for len(records) >= MaxRecords {
		if !evictTerminal(records, "") {
			return Snapshot{}, ErrLimit
		}
	}
	records[s.ID] = s.Clone()
	return s.Clone(), nil
}

// ClaimRecord also fences uncertain effects left by an interrupted process.
// No expired in-flight transfer can return to the runnable queue.
func ClaimRecord(records map[string]Snapshot, worker string, now time.Time, lease time.Duration) (Snapshot, error) {
	if !validID(worker) || lease < time.Second || lease > time.Hour {
		return Snapshot{}, ErrInvalid
	}
	now = normalized(now)
	PruneRecords(records, now)
	candidates := []Snapshot{}
	served := map[string]time.Time{}
	for _, s := range records {
		if s.LastServedAt.After(served[s.Scope.ActorID]) {
			served[s.Scope.ActorID] = s.LastServedAt
		}
	}
	for id, s := range records {
		if s.Terminal() {
			continue
		}
		if s.State == Running && now.Before(s.LeaseUntil) {
			continue
		}
		if s.InFlight {
			s.Items = append(s.Items, ItemResult{Index: s.NextIndex, State: Unknown, Code: "OutcomeUnknown"})
			terminal(&s, Unknown, "OutcomeUnknown", now)
			records[id] = s
			continue
		}
		if s.CancelRequested {
			terminal(&s, Cancelled, "", now)
			records[id] = s
			continue
		}
		if !now.Before(s.Deadline) {
			terminal(&s, Failed, "AuthorizationExpired", now)
			records[id] = s
			continue
		}
		candidates = append(candidates, s)
	}
	if len(candidates) == 0 {
		return Snapshot{}, ErrNotFound
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		aa, bb := served[a.Scope.ActorID], served[b.Scope.ActorID]
		if !aa.Equal(bb) {
			return aa.Before(bb)
		}
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt)
		}
		return a.ID < b.ID
	})
	s := candidates[0]
	s.State = Running
	s.WorkerID = worker
	s.LeaseID = securetoken.New()
	s.LeaseUntil = now.Add(lease)
	s.LastServedAt = now
	s.UpdatedAt = now
	s.Revision++
	records[s.ID] = s.Clone()
	return s.Clone(), nil
}

func SaveRecord(s Snapshot, lease string, revision uint64, c Checkpoint, now time.Time) (Snapshot, error) {
	now = normalized(now)
	if s.State != Running || s.LeaseID != lease || s.Revision != revision || !now.Before(s.LeaseUntil) {
		return Snapshot{}, ErrConflict
	}
	commands := 0
	if c.Yield {
		commands++
	}
	if c.Begin {
		commands++
	}
	if c.Transfer != nil {
		commands++
	}
	if c.Page != nil {
		commands++
	}
	if c.FailCode != "" {
		commands++
	}
	if commands != 1 || !safeCode(c.FailCode) {
		return Snapshot{}, ErrInvalid
	}
	s = s.Clone()
	s.UpdatedAt = now
	if c.Yield {
		if s.InFlight {
			return Snapshot{}, ErrInvalid
		}
		if s.CancelRequested {
			terminal(&s, Cancelled, "", now)
		} else {
			yield(&s)
			s.Revision++
		}
		return s, nil
	}
	if c.FailCode != "" {
		if s.InFlight {
			s.Items = append(s.Items, ItemResult{Index: s.NextIndex, State: Unknown, Code: "OutcomeUnknown"})
			terminal(&s, Unknown, "OutcomeUnknown", now)
		} else if s.CancelRequested {
			terminal(&s, Cancelled, "", now)
		} else {
			terminal(&s, Failed, c.FailCode, now)
		}
		return s, nil
	}
	if c.Begin {
		if s.Kind != "transfer" || s.InFlight || s.NextIndex >= len(s.Spec.Transfers) {
			return Snapshot{}, ErrInvalid
		}
		if s.CancelRequested {
			terminal(&s, Cancelled, "", now)
			return s, nil
		}
		if !now.Before(s.Deadline) {
			terminal(&s, Failed, "AuthorizationExpired", now)
			return s, nil
		}
		s.InFlight = true
		s.Revision++
		return s, nil
	}
	if c.Transfer != nil {
		if s.Kind != "transfer" || !s.InFlight || s.NextIndex >= len(s.Spec.Transfers) || !safeCode(c.Transfer.Code) || c.Transfer.Moved && (!c.Transfer.Copied || !s.Spec.Transfers[s.NextIndex].Move) {
			return Snapshot{}, ErrInvalid
		}
		r := c.Transfer
		state := "success"
		if r.Uncertain {
			state = Unknown
		} else if !r.Copied || s.Spec.Transfers[s.NextIndex].Move && !r.Moved {
			state = "partial"
		}
		s.Items = append(s.Items, ItemResult{Index: s.NextIndex, State: state, Copied: r.Copied, Moved: r.Moved, Code: r.Code})
		s.InFlight = false
		if r.Uncertain {
			terminal(&s, Unknown, "OutcomeUnknown", now)
			return s, nil
		}
		s.NextIndex++
		if s.NextIndex == len(s.Spec.Transfers) {
			terminal(&s, Completed, "", now)
			return s, nil
		}
		if s.CancelRequested {
			terminal(&s, Cancelled, "", now)
			return s, nil
		}
		yield(&s)
		s.Revision++
		return s, nil
	}
	p := c.Page
	if s.Kind != "scan" || p.Objects < 0 || p.Objects > 500 || p.Bytes < 0 || len(p.NextToken) > 16384 || s.Scan.Pages >= MaxScanPages || p.Objects > math.MaxInt64-s.Scan.Objects || p.Bytes > math.MaxInt64-s.Scan.Bytes {
		return Snapshot{}, ErrInvalid
	}
	if p.NextToken != "" {
		h := tokenHash(p.NextToken)
		for _, seen := range s.Scan.Seen {
			if seen == h {
				return Snapshot{}, ErrInvalid
			}
		}
		s.Scan.Seen = append(s.Scan.Seen, h)
	}
	s.Scan.Objects += p.Objects
	s.Scan.Bytes += p.Bytes
	s.Scan.Pages++
	s.Scan.Token = p.NextToken
	if p.NextToken == "" {
		terminal(&s, Completed, "", now)
		return s, nil
	}
	if s.CancelRequested {
		terminal(&s, Cancelled, "", now)
		return s, nil
	}
	yield(&s)
	s.Revision++
	return s, nil
}

func yield(s *Snapshot) {
	s.State = Queued
	s.WorkerID = ""
	s.LeaseID = ""
	s.LeaseUntil = time.Time{}
}

func CancelRecord(s Snapshot, revision uint64, now time.Time) (Snapshot, error) {
	if revision != s.Revision {
		return Snapshot{}, ErrConflict
	}
	if s.Terminal() {
		return s.Clone(), nil
	}
	s = s.Clone()
	if s.State == Queued {
		terminal(&s, Cancelled, "", now)
	} else {
		s.CancelRequested = true
		s.UpdatedAt = normalized(now)
		s.Revision++
	}
	return s, nil
}
func RetryRecord(s Snapshot, revision uint64, now time.Time) (Snapshot, error) {
	if revision != s.Revision || !s.Retryable() || !now.Before(s.Deadline) {
		return Snapshot{}, ErrConflict
	}
	s = s.Clone()
	s.State = Queued
	s.Code = ""
	s.CancelRequested = false
	s.ExpiresAt = time.Time{}
	s.UpdatedAt = normalized(now)
	s.Revision++
	return s, nil
}
func PruneRecords(records map[string]Snapshot, now time.Time) {
	for id, s := range records {
		if s.Terminal() && !now.Before(s.ExpiresAt) {
			delete(records, id)
		}
	}
}

func evictTerminal(records map[string]Snapshot, actor string) bool {
	oldest := ""
	var stamp time.Time
	for id, s := range records {
		if !s.Terminal() || actor != "" && s.Scope.ActorID != actor {
			continue
		}
		if oldest == "" || s.UpdatedAt.Before(stamp) || s.UpdatedAt.Equal(stamp) && id < oldest {
			oldest = id
			stamp = s.UpdatedAt
		}
	}
	if oldest == "" {
		return false
	}
	delete(records, oldest)
	return true
}

func ValidBackup(records []Snapshot) bool {
	if len(records) > MaxRecords {
		return false
	}
	ids := map[string]bool{}
	actors := map[string]int{}
	size := 0
	for _, s := range records {
		if !Valid(s) || ids[s.ID] {
			return false
		}
		ids[s.ID] = true
		actors[s.Scope.ActorID]++
		if actors[s.Scope.ActorID] > MaxPerActor {
			return false
		}
		raw, err := json.Marshal(s)
		if err != nil || len(raw) > MaxSnapshotBytes-size {
			return false
		}
		size += len(raw)
	}
	return true
}
