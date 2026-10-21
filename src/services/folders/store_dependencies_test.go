package folders

import (
	"context"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/folders/share"
)

type publicFolderStore struct {
	grant *foldershare.FolderShare
	token string
}

func (s *publicFolderStore) Get(token string) *foldershare.FolderShare {
	s.token = token
	return s.grant
}
func (s *publicFolderStore) IssueTicket(*foldershare.FolderShare, string, string) (string, error) {
	panic("unexpected ticket issue")
}

type folderAdmission struct {
	owner           string
	calls, releases int
}

func (a *folderAdmission) AcquireTransfer(owner string) (func(), error) {
	a.owner = owner
	a.calls++
	return func() { a.releases++ }, nil
}

func TestPublicFolderPortPreservesScopeAndReadRelease(t *testing.T) {
	grant := foldershare.New(foldershare.Config{Owner: "owner", Bucket: "test-bucket", Prefix: "reports/", Expires: time.Now().Add(time.Hour)})
	store := &publicFolderStore{grant: grant}
	admission := &folderAdmission{}
	value, err := ReadPublicFolder(context.Background(), store, admission, "link-token", PublicFolderOptions{Mode: "info"})
	if err != nil || value.Info == nil || value.Info.Bucket != grant.Bucket() || value.Info.Prefix != grant.Prefix() || !value.Info.ExpiresAt.Equal(grant.Expires()) || store.token != "link-token" || admission.owner != "owner" || admission.calls != 1 || admission.releases != 1 {
		t.Fatal("delegated read scope or lifetime lost", value, err)
	}
	_, err = ReadPublicFolder(context.Background(), store, admission, "link-token", PublicFolderOptions{Mode: "list", Prefix: "other/"})
	if err == nil || apperror.Public(err).Code != "FolderScopeDenied" || admission.releases != 2 {
		t.Fatal("scope failure leaked read", err)
	}
	grant.Stop()
	if _, err = ReadPublicFolder(context.Background(), store, admission, "link-token", PublicFolderOptions{}); err == nil || apperror.Public(err).Code != "FolderShareExpired" || admission.calls != 2 {
		t.Fatal("revoked grant admitted", err)
	}
	store.grant = nil
	if _, err = ReadPublicFolder(context.Background(), store, admission, "missing", PublicFolderOptions{}); err == nil || apperror.Public(err).Status != 404 {
		t.Fatal("missing grant exposed", err)
	}
}

type fileTicketReader struct {
	id      string
	consume bool
}

func (s *fileTicketReader) TakeTicket(id string, consume bool) (foldershare.FolderFileTicket, bool) {
	s.id, s.consume = id, consume
	return foldershare.FolderFileTicket{}, false
}

func TestSharedFilePortPreservesTicketConsumptionMode(t *testing.T) {
	for _, consume := range []bool{false, true} {
		store := &fileTicketReader{}
		admission := &folderAdmission{}
		_, err := OpenSharedFile(context.Background(), store, admission, "file-ticket", consume, "")
		if err == nil || apperror.Public(err).Code != "DownloadExpired" || store.id != "file-ticket" || store.consume != consume || admission.calls != 0 {
			t.Fatal("ticket mode changed or missing ticket admitted", err)
		}
	}
}
