package runtime_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/iam"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/tests/testsession"
)

func TestSessionOwnsEndpointsAndReturnsDetachedValues(t *testing.T) {
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if !strings.Contains(r.Header.Get("Authorization"), "Credential=original-key/") || !strings.Contains(r.Header.Get("Authorization"), "/us-east-1/s3/aws4_request") {
			t.Error("original credentials or signing region lost")
		}
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<ListAllMyBucketsResult><Buckets></Buckets></ListAllMyBucketsResult>`)
	}))
	defer upstream.Close()
	public, _ := url.Parse("https://public.example.test")
	internal, _ := url.Parse(upstream.URL)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	connection := runtime.Connection{AccessKey: "original-key", SecretKey: "original-secret", Region: "us-east-1"}
	s, err := runtime.NewConnectedSession(connection, config.Config{SessionTTL: time.Hour, Origin: "https://ui.example"}, public, internal, transport)
	if err != nil {
		t.Fatal(err)
	}
	client := runtime.NewClients(s)
	// Both the input URLs and all returned URLs must be detached from live state.
	public.Host, internal.Host = "changed-public.example", "changed-internal.example"
	public.Scheme, internal.Scheme = "http", "https"
	connection.AccessKey, connection.SecretKey, connection.Region = "changed-key", "changed-secret", "changed-region"
	client.PublicEndpoint().Host = "changed-view.example"
	client.InternalEndpoint().Host = "changed-view.example"
	if client.PublicEndpoint().String() != "https://public.example.test" || client.InternalEndpoint().String() != upstream.URL || s.InternalHost() != strings.TrimPrefix(upstream.URL, "http://") {
		t.Fatal("mutable endpoint escaped session ownership")
	}
	link, err := client.PresignedGetObject(context.Background(), "test-bucket", "file.txt", time.Minute, nil)
	if err != nil || link.Host != "public.example.test" || link.Scheme != "https" {
		t.Fatal("signer changed", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := client.ListBuckets(ctx); err != nil || requests.Load() != 1 {
		t.Fatal("request endpoint changed", err)
	}

	original := s.CookieSnapshot()
	view := s.CookieSnapshot()
	view.ID, view.Token, view.Secure, view.Expires = "changed", "changed", false, time.Time{}
	if s.CookieSnapshot() != original || s.SessionID() != original.ID || s.ExpiresAt() != original.Expires || !original.Secure {
		t.Fatal("cookie snapshot changed the live session")
	}
	if !s.MatchesToken(original.Token) || s.MatchesToken(view.Token) || !s.MatchesCSRF(s.CSRFToken()) || s.MatchesCSRF("wrong") || s.ActorName() != "original-key" {
		t.Fatal("authentication state changed")
	}
	if s.Expired(s.ExpiresAt()) || !s.Expired(s.ExpiresAt().Add(time.Nanosecond)) {
		t.Fatal("expiry boundary changed")
	}
	for _, value := range []any{s, s.CookieSnapshot(), s.Actions()} {
		raw, err := json.Marshal(value)
		if err != nil || string(raw) != "{}" {
			t.Fatal("private session state became JSON", err)
		}
	}
}

const sessionPolicy = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::data/*"]}]}`

func TestSessionActionSnapshotsAreCoherentAndDetached(t *testing.T) {
	var denied atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if denied.Load() {
			w.WriteHeader(403)
			fmt.Fprint(w, `{"Code":"AccessDenied"}`)
			return
		}
		fmt.Fprintf(w, `{"AccountName":"fixture","Policy":%s}`, sessionPolicy)
	}))
	defer upstream.Close()
	s := testsession.New(t, upstream.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	query := iam.Query{Action: "s3:GetObject", Bucket: "data", Key: "file.txt"}
	if s.Actions().Known() || s.Actions().Allows(query) || s.Actions().Issue() != nil {
		t.Fatal("new session inferred permissions")
	}
	if issue := s.RefreshActionPolicy(ctx); issue != nil {
		t.Fatal(issue)
	}
	allowed := s.Actions()
	if !allowed.Known() || !allowed.Allows(query) || allowed.Issue() != nil {
		t.Fatal("known policy missing")
	}
	denied.Store(true)
	issue := s.RefreshActionPolicy(ctx)
	if issue == nil || issue.Code != "AccountInfoDenied" {
		t.Fatal(issue)
	}
	issue.Code, issue.Message = "changed", messages.Literal("changed")
	failed := s.Actions()
	copy := failed.Issue()
	if copy == nil || copy.Code != "AccountInfoDenied" {
		t.Fatal("returned refresh issue aliases the cache")
	}
	copy.Code = "changed-again"
	if failed.Known() || failed.Allows(query) || failed.Issue().Code != "AccountInfoDenied" || s.Actions().Issue().Code != "AccountInfoDenied" {
		t.Fatal("snapshot issue aliases live state or stale permissions survived failure")
	}
	if !allowed.Known() || !allowed.Allows(query) || allowed.Issue() != nil {
		t.Fatal("old snapshot changed after publication")
	}

	var workers sync.WaitGroup
	for range 2 {
		workers.Go(func() {
			for i := range 12 {
				denied.Store(i%2 == 0)
				_ = s.RefreshActionPolicy(ctx)
			}
		})
	}
	for range 4 {
		workers.Go(func() {
			for range 2000 {
				snapshot := s.Actions()
				if snapshot.Known() {
					if snapshot.Issue() != nil || !snapshot.Allows(query) {
						t.Error("mixed successful snapshot")
					}
				} else if snapshot.Issue() == nil || snapshot.Allows(query) {
					t.Error("mixed failed snapshot")
				}
			}
		})
	}
	workers.Wait()
	denied.Store(false)
	if issue := s.RefreshActionPolicy(ctx); issue != nil || !s.Actions().Known() || s.Actions().Issue() != nil {
		t.Fatal("recovery did not clear cached issue", issue)
	}
}

func TestSessionAccessLockRemainsSharedByAdapters(t *testing.T) {
	s := testsession.New(t, "http://fixture.example")
	one, two := runtime.NewClients(s), runtime.NewClients(s)
	release, err := one.LockAccess(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	started, acquired := make(chan struct{}), make(chan struct{})
	go func() {
		close(started)
		unlock, err := two.LockAccess(context.Background())
		if err != nil {
			t.Error(err)
			return
		}
		defer unlock()
		close(acquired)
	}()
	<-started
	select {
	case <-acquired:
		t.Fatal("adapters no longer share an access lock")
	case <-time.After(20 * time.Millisecond):
	}
	release()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("access lock was not released")
	}
}
