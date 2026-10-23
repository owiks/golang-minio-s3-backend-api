package multipart

import (
	"context"
	"testing"
	"time"
)

type permanentClient struct{ *fixtureClient }

func (permanentClient) SessionExpiresAt() time.Time { return time.Time{} }

func TestPermanentCredentialKeepsFiniteMultipartLifetime(t *testing.T) {
	store := &memoryStore{}
	before := time.Now()
	s, err := Create(context.Background(), store, permanentClient{newFixture()}, CreateRequest{Bucket: "fixture-bucket", Key: "file.bin", Revision: "absent", Size: 1})
	if err != nil {
		t.Fatal("permanent credential could not create upload", err)
	}
	if !s.ExpiresAt.After(before) || s.ExpiresAt.After(time.Now().Add(TTL)) || s.ExpiresAt.Before(before.Add(TTL)) {
		t.Fatal("multipart lifetime is not bounded by its own TTL", s.ExpiresAt)
	}
	if _, release, err := acquire(context.Background(), store, permanentClient{newFixture()}, s.ID); err != nil {
		t.Fatal("permanent upload was immediately expired", err)
	} else {
		release()
	}
}
