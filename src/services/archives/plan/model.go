package archiveplan

import (
	"context"
	"sync"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	objectstate "github.com/owiks/golang-minio-s3-backend-api/src/services/objects/state"
)

// Config is detached preparation data; New copies its slices and maps.
type Config struct {
	SessionID, Bucket, Name, Base, RelativeBase string
	Created                                     time.Time
	Items                                       []ArchiveItem
	Issues                                      map[string]messages.Ref
	AllowPartial                                bool
}
type ArchivePlan struct {
	mu                                          sync.Mutex
	sessionID, bucket, name, base, relativeBase string
	created                                     time.Time
	started, finished, cancelled, allowPartial  bool
	items                                       []ArchiveItem
	issues                                      map[string]messages.Ref
	cancel                                      context.CancelFunc
	outcome, code                               string
}

func New(c Config) *ArchivePlan {
	return &ArchivePlan{sessionID: c.SessionID, bucket: c.Bucket, name: c.Name, base: c.Base, relativeBase: c.RelativeBase, created: c.Created, items: append([]ArchiveItem{}, c.Items...), issues: copyIssues(c.Issues), allowPartial: c.AllowPartial}
}
func copyIssues(in map[string]messages.Ref) map[string]messages.Ref {
	out := map[string]messages.Ref{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
func (p *ArchivePlan) SessionID() string  { return p.sessionID }
func (p *ArchivePlan) Bucket() string     { return p.bucket }
func (p *ArchivePlan) Created() time.Time { return p.created }
func (p *ArchivePlan) ItemCount() int     { p.mu.Lock(); defer p.mu.Unlock(); return len(p.items) }
func (p *ArchivePlan) Available(owner string, now time.Time) bool {
	return p.sessionID == owner && now.Sub(p.created) <= time.Hour
}
func (p *ArchivePlan) snapshotLocked(ticket string) ArchiveSnapshot {
	count := 0
	var size, sent int64
	for _, item := range p.items {
		if item.Info.ETag != "" {
			count++
			size += item.Size
		}
		sent += item.Sent
	}
	return ArchiveSnapshot{Ticket: ticket, Name: p.name, Base: p.base, RelativeBase: p.relativeBase, Count: count, Size: size, Sent: sent, Started: p.started, Finished: p.finished, Cancelled: p.cancelled, Items: append([]ArchiveItem{}, p.items...), Issues: copyIssues(p.issues), ExpiresAt: p.created.Add(time.Hour)}
}
func (p *ArchivePlan) Snapshot(ticket string) ArchiveSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.snapshotLocked(ticket)
}
func (p *ArchivePlan) Configure(ticket, name, base string, allow bool) (ArchiveSnapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started || p.finished {
		return ArchiveSnapshot{}, apperror.New(409, "ArchiveStarted", messages.ArchivesArchiveStartedModel)
	}
	name, err := ArchiveFilename(name)
	if err != nil {
		return ArchiveSnapshot{}, err
	}
	if base != "" && base != p.relativeBase {
		return ArchiveSnapshot{}, apperror.New(400, "InvalidArchiveBase", messages.ArchivesInvalidArchiveBase)
	}
	paths, err := ArchivePaths(p.items, base)
	if err != nil {
		return ArchiveSnapshot{}, err
	}
	p.name, p.base, p.allowPartial = name, base, allow
	for i := range p.items {
		p.items[i].Path = paths[i]
	}
	return p.snapshotLocked(ticket), nil
}
func (p *ArchivePlan) PrepareForget() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started && !p.finished {
		return apperror.New(409, "ArchiveRunning", messages.ArchivesArchiveRunning)
	}
	if !p.started {
		p.cancelled, p.finished = true, true
	}
	return nil
}
func (p *ArchivePlan) Cancel(ticket string) ArchiveSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.finished {
		return p.snapshotLocked(ticket)
	}
	p.cancelled = true
	if p.cancel != nil {
		p.cancel()
	}
	if !p.started {
		p.finished = true
		for i := range p.items {
			if p.items[i].State == "pending" {
				p.items[i].State = "cancelled"
				p.items[i].Message = messages.ArchivesTheTransferWasNotStarted
			}
		}
	}
	return p.snapshotLocked(ticket)
}
func (p *ArchivePlan) Abort() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.finished {
		p.cancelled = true
		if p.cancel != nil {
			p.cancel()
		}
	}
}
func (p *ArchivePlan) BeginDownload(parent context.Context) (context.Context, ArchiveSnapshot, context.CancelFunc, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started || p.cancelled || p.finished {
		return nil, ArchiveSnapshot{}, nil, apperror.New(409, "ArchiveStarted", messages.ArchivesArchiveStarted)
	}
	count := 0
	partial := len(p.issues) > 0
	for _, item := range p.items {
		if item.State == "error" {
			partial = true
		} else {
			count++
		}
	}
	if count == 0 {
		return nil, ArchiveSnapshot{}, nil, apperror.New(400, "EmptyArchive", messages.ArchivesEmptyArchive)
	}
	if partial && !p.allowPartial {
		return nil, ArchiveSnapshot{}, nil, apperror.New(409, "ArchivePartialConsent", messages.ArchivesArchivePartialConsent)
	}
	ctx, cancel := context.WithCancel(parent)
	p.cancel, p.started = cancel, true
	return ctx, p.snapshotLocked(""), cancel, nil
}
func (p *ArchivePlan) FailItem(i int, message messages.Ref) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started && !p.finished && i >= 0 && i < len(p.items) {
		p.items[i].State = "error"
		p.items[i].Message = message
	}
}
func (p *ArchivePlan) CompleteItem(i int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started && !p.finished && i >= 0 && i < len(p.items) {
		p.items[i].State = "done"
		p.items[i].Message = nil
	}
}
func (p *ArchivePlan) AddSent(i int, n int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started && !p.finished && i >= 0 && i < len(p.items) && n > 0 {
		p.items[i].Sent += n
	}
}
func (p *ArchivePlan) Finish(ctxErr error, finalized bool) (string, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.started {
		return "error", "ArchiveNotStarted"
	}
	if p.finished {
		return p.outcome, p.code
	}
	result, code := "success", ""
	if ctxErr != nil || p.cancelled {
		result, code = "cancelled", "ArchiveCancelled"
	} else if !finalized {
		result, code = "error", "ArchiveIncomplete"
	} else {
		if len(p.issues) > 0 {
			result, code = "partial", "ArchivePartial"
		}
		for _, item := range p.items {
			if item.State == "error" {
				result, code = "partial", "ArchivePartial"
			}
		}
	}
	p.finished = true
	p.cancel = nil
	p.outcome, p.code = result, code
	for i := range p.items {
		item := &p.items[i]
		if item.State == "pending" || !finalized && item.State == "done" {
			item.State = "error"
			item.Message = messages.ArchivesZIPCompletionWasNotConfirmedPrepareA
			if p.cancelled {
				item.State = "cancelled"
				item.Message = messages.ArchivesTheTransferWasStoppedTheZIPMay
			}
		}
	}
	return result, code
}

type ArchiveItem struct {
	Key     string                       `json:"key"`
	Path    string                       `json:"path"`
	Size    int64                        `json:"size"`
	Sent    int64                        `json:"sent"`
	State   string                       `json:"state"`
	Message messages.Ref                 `json:"message,omitempty"`
	Info    objectstate.DeletionSnapshot `json:"-"`
}
type ArchiveSnapshot struct {
	Ticket       string                  `json:"ticket"`
	Name         string                  `json:"name"`
	Base         string                  `json:"base"`
	RelativeBase string                  `json:"relativeBase"`
	Count        int                     `json:"count"`
	Size         int64                   `json:"size"`
	Sent         int64                   `json:"sent"`
	Started      bool                    `json:"started"`
	Finished     bool                    `json:"finished"`
	Cancelled    bool                    `json:"cancelled"`
	Items        []ArchiveItem           `json:"items"`
	Issues       map[string]messages.Ref `json:"issues"`
	ExpiresAt    time.Time               `json:"expiresAt"`
}
