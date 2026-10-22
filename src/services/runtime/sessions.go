package runtime

import (
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	settings "github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/coordination"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/iam"
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
	accessLock                     coordination.Locks
	actionMu                       sync.RWMutex
	actionPolicy                   *iam.Policy
	actionIssue                    *iam.ActionCheckIssue
	id, token, csrf, actor, region string
	bearer                         bool
	bearerDigest                   [32]byte
	conditionalDelete              bool
	conditionalComplete            bool
	secureCookies                  bool
	expires                        time.Time
	public, internal               *url.URL
	s3, signer                     *minio.Client
	admin                          *storage.AdminClient
	transport                      *http.Transport
	credentials                    *credentials.Credentials
}

// State composes independent owners of runtime resources. Collection locks and
// storage maps are private to each store; lifecycle coordination lives here.
type State struct {
	config                         settings.Config
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
	c = c.WithDefaults().Clone()
	sessions := NewSessionStore()
	temporary := staging.New(c.TemporaryBytes)
	return &State{
		config: c, Sessions: sessions, Temporary: temporary,
		Archives: NewArchiveStore(sessions.Has), Invites: NewUploadInviteStore(),
		Shares: NewFolderShareStore(), ZIPs: NewZIPCache(temporary, sessions.Has),
		Capabilities:    NewCapabilityStore(),
		Multipart:       NewMultipartStore(sessions.Has),
		revisions:       NewAccessRevisions(),
		logins:          coordination.NewAdmission(c.Limits.Logins, 2),
		loginAttempts:   coordination.NewAttempts(c.Limits.LoginBurst, 4096, c.Limits.LoginInterval),
		loginIdentities: coordination.NewAttempts(20, 4096, 3*time.Second),
		transfers:       coordination.NewAdmission(c.Limits.Transfers, min(c.Limits.TransfersPerSession, c.Limits.Transfers)),
	}
}

// Config returns an independent snapshot; runtime settings are fixed at New.
func (app *State) Config() settings.Config { return app.config.Clone() }
