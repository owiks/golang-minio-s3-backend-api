package runtime

import (
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	settings "github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/secretbox"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/commandlog"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/coordination"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/iam"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/operations"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/staging"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
)

type Connection struct {
	Endpoint     string `json:"endpoint" validate:"required"`
	AccessKey    string `json:"accessKey" validate:"required,maxBytes=256"`
	SecretKey    string `json:"secretKey" validate:"required,maxBytes=4096"`
	Region       string `json:"region"`
	SessionToken string `json:"sessionToken,omitempty"`
}
type Session struct {
	*sessionCore
	id, token, csrf string
	bearer          bool
	bearerDigest    [32]byte
	expires         time.Time
	access          authstate.Access
	previous        bool
	manager         *authstate.Manager
	boundS3         *minio.Client
	boundAdmin      *storage.AdminClient
}
type sessionCore struct {
	accessLock                                            coordination.Locks
	actionMu                                              sync.RWMutex
	actionPolicy                                          *iam.Policy
	actionIssue                                           *iam.ActionCheckIssue
	actor, region                                         string
	conditionalDelete, conditionalComplete, secureCookies bool
	public, internal                                      *url.URL
	s3, signer                                            *minio.Client
	admin                                                 *storage.AdminClient
	transport                                             *http.Transport
	credentials                                           *credentials.Credentials
	connection                                            Connection
	connectionID                                          string
	connectionRevision                                    int64
	secretVersion                                         int64
}

// State composes independent owners of runtime resources. Collection locks and
// storage maps are private to each store; lifecycle coordination lives here.
type State struct {
	config                         settings.Config
	Auth                           *authstate.Manager    `json:"-"`
	Commands                       commandlog.Repository `json:"-"`
	Operations                     operations.Repository `json:"-"`
	operationMonitor               *operations.Monitor
	Sessions                       *SessionStore      `json:"-"`
	Archives                       *ArchiveStore      `json:"-"`
	Invites                        *UploadInviteStore `json:"-"`
	Shares                         *FolderShareStore  `json:"-"`
	ZIPs                           *ZIPCache          `json:"-"`
	Capabilities                   *CapabilityStore   `json:"-"`
	Multipart                      *MultipartStore    `json:"-"`
	Temporary                      *staging.Budget    `json:"-"`
	revisions                      *AccessRevisions
	closeOnce                      sync.Once
	mutations                      coordination.Locks
	logins, transfers              *coordination.Admission
	loginAttempts, loginIdentities *coordination.Attempts
}

func New(c settings.Config) *State {
	box, err := secretbox.Ephemeral()
	if err != nil {
		panic("secure state encryption unavailable")
	}
	return NewWithAuth(c, authstate.NewManager(authstate.NewMemory(), box, c.WithDefaults().TokenTTL))
}
func NewWithAuth(c settings.Config, manager *authstate.Manager) *State {
	return NewWithCommandLog(c, manager, commandlog.NewMemory())
}

func NewWithCommandLog(c settings.Config, manager *authstate.Manager, commands commandlog.Repository) *State {
	return NewWithOperations(c, manager, commands, operations.NewMemory())
}

func NewWithOperations(c settings.Config, manager *authstate.Manager, commands commandlog.Repository, jobs operations.Repository) *State {
	c = c.WithDefaults().Clone()
	sessions := NewSessionStore()
	temporary := staging.New(c.TemporaryBytes)
	app := &State{
		config: c, Auth: manager, Commands: commands, Operations: jobs, Sessions: sessions, Temporary: temporary,
		operationMonitor: operations.NewMonitor(),
		Invites:          NewUploadInviteStore(),
		Shares:           NewFolderShareStore(),
		Capabilities:     NewCapabilityStore(),

		revisions:       NewAccessRevisions(),
		logins:          coordination.NewAdmission(c.Limits.Logins, 2),
		loginAttempts:   coordination.NewAttempts(c.Limits.LoginBurst, 4096, c.Limits.LoginInterval),
		loginIdentities: coordination.NewAttempts(20, 4096, 3*time.Second),
		transfers:       coordination.NewAdmission(c.Limits.Transfers, min(c.Limits.TransfersPerSession, c.Limits.Transfers)),
	}
	app.Archives = NewArchiveStore(app.OwnerRetained)
	app.ZIPs = NewZIPCache(temporary, app.OwnerRetained)
	app.Multipart = NewMultipartStore(app.OwnerRetained)
	return app
}

// Config returns an independent snapshot; runtime settings are fixed at New.
func (app *State) Config() settings.Config { return app.config.Clone() }
