package runtime

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7/pkg/credentials"

	settings "minio-web/api/src/config"
	"minio-web/api/src/services/securetoken"
	"minio-web/api/src/services/storage"
)

// NewConnectedSession constructs native clients after validation. The caller
// owns tr until the session is successfully published in its registry.
func NewConnectedSession(c Connection, cfg settings.Config, public, internal *url.URL, tr *http.Transport) (*Session, error) {
	// Endpoints are caller-owned mutable values; keep independent session copies.
	public, internal = copyEndpoint(public), copyEndpoint(internal)
	c.Region = strings.TrimSpace(c.Region)
	if c.Region == "" {
		c.Region = "us-east-1"
	}
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
	return &Session{
		conditionalDelete: storage.NativeConditionalDelete(public) || cfg.ConditionalDeleteEndpoints[public.String()] || cfg.ConditionalDeleteEndpoints[internal.String()],
		// Conditional completion is an explicit operator assertion for the
		// checked endpoint, never inferred from a hostname or successful PUT.
		conditionalComplete: cfg.ConditionalCompleteEndpoints[public.String()] || cfg.ConditionalCompleteEndpoints[internal.String()],
		id:                  securetoken.New(), token: securetoken.New(), csrf: securetoken.New(), actor: c.AccessKey,
		secureCookies: strings.HasPrefix(cfg.Origin, "https://"), region: c.Region, expires: time.Now().Add(cfg.SessionTTL),
		public: public, internal: internal, s3: s3, signer: signer, admin: admin, transport: tr, credentials: creds,
	}, nil
}
