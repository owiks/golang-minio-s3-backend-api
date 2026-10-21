package runtime

import (
	"context"
	"net/url"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/iam"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
)

// Clients adapts the authenticated connection to consumer-owned interfaces.
// Business functions receive those interfaces, never this concrete connection.
// Credentials and the mutable session remain private to the adapter.
type Clients struct {
	*storage.S3Client    `json:"-"`
	*storage.AdminClient `json:"-"`
	session              *Session
}

func NewClients(s *Session) *Clients {
	if s == nil {
		return nil
	}
	return &Clients{S3Client: storage.NewS3Client(s.s3), AdminClient: s.admin, session: s}
}
func (c *Clients) SessionID() string          { return c.session.id }
func (c *Clients) ActorName() string          { return c.session.actor }
func (c *Clients) RegionName() string         { return c.session.region }
func (c *Clients) PublicEndpoint() *url.URL   { return copyEndpoint(c.session.public) }
func (c *Clients) InternalEndpoint() *url.URL { return copyEndpoint(c.session.internal) }
func (c *Clients) RefreshActionPolicy(ctx context.Context) *iam.ActionCheckIssue {
	return c.session.RefreshActionPolicy(ctx)
}

// Published policies are immutable. Take one snapshot for a multi-action check.
func (c *Clients) ActionPolicySnapshot() *iam.Policy {
	return c.session.Actions().policy
}
func (c *Clients) PresignedGetObject(ctx context.Context, bucket, key string, expires time.Duration, params url.Values) (*url.URL, error) {
	return c.session.signer.PresignedGetObject(ctx, bucket, key, expires, params)
}
func (c *Clients) ConditionalDeleteSupported() bool { return c.session.conditionalDelete }
func (c *Clients) RemoveConditional(ctx context.Context, bucket, key, etag string) error {
	return storage.ConditionalDelete(ctx, c.session.clientConfig(), c.session.transport, bucket, key, etag, true)
}

func (c *Clients) LockAccess(ctx context.Context) (func(), error) {
	return c.session.accessLock.Acquire(ctx, "discovery")
}
