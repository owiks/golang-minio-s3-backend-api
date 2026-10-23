package runtime

import (
	"crypto/sha256"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7/pkg/credentials"

	settings "github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
)

// NewConnectedSession constructs native clients after validation. The caller
// owns tr until the session is successfully published in its registry.
func NewConnectedSession(c Connection, cfg settings.Config, public, internal *url.URL, tr *http.Transport) (*Session, error) {
	s, err := newSession(c, cfg, public, internal, tr)
	if err != nil {
		return nil, err
	}
	s.token, s.csrf = securetoken.New(), securetoken.New()
	return s, nil
}

// NewBearerSession returns the credential once; the session retains only its
// digest. Its authentication mode cannot be changed after publication.
func NewBearerSession(c Connection, cfg settings.Config, public, internal *url.URL, tr *http.Transport) (*Session, string, error) {
	s, err := newSession(c, cfg, public, internal, tr)
	if err != nil {
		return nil, "", err
	}
	token := securetoken.New()
	s.bearer, s.bearerDigest = true, sha256.Sum256([]byte(token))
	if cfg.SessionTTL == 0 {
		s.expires = time.Time{}
	}
	return s, token, nil
}

func newSession(c Connection, cfg settings.Config, public, internal *url.URL, tr *http.Transport) (*Session, error) {
	// Endpoints are caller-owned mutable values; keep independent session copies.
	public, internal = copyEndpoint(public), copyEndpoint(internal)
	c.Region = strings.TrimSpace(c.Region)
	if c.Region == "" {
		c.Region = "us-east-1"
	}
	c.Endpoint = public.String()
	creds := credentials.NewStaticV4(c.AccessKey, c.SecretKey, c.SessionToken)
	clientConfig := storage.Config{Endpoint: internal, Credentials: creds, Region: c.Region}
	s3, err := storage.NewS3(clientConfig, tr)
	if err != nil {
		return nil, err
	}
	signer, err := storage.NewSigner(storage.Config{Endpoint: public, Credentials: creds, Region: c.Region})
	if err != nil {
		return nil, err
	}
	admin, err := storage.NewAdmin(clientConfig, tr)
	if err != nil {
		return nil, err
	}
	return &Session{id: securetoken.New(), expires: time.Now().Add(cfg.SessionTTL), sessionCore: &sessionCore{
		conditionalDelete: storage.NativeConditionalDelete(public) || cfg.ConditionalDeleteEndpoints[public.String()] || cfg.ConditionalDeleteEndpoints[internal.String()],
		// Conditional completion is an explicit operator assertion for the
		// checked endpoint, never inferred from a hostname or successful PUT.
		conditionalComplete: cfg.ConditionalCompleteEndpoints[public.String()] || cfg.ConditionalCompleteEndpoints[internal.String()],
		actor:               c.AccessKey, connection: c, connectionID: securetoken.New(), connectionRevision: 1, secretVersion: 1,
		secureCookies: strings.HasPrefix(cfg.Origin, "https://"), region: c.Region,
		public: public, internal: internal, s3: s3, signer: signer, admin: admin, transport: tr, credentials: creds,
	}}, nil
}
