package identities

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/minio/madmin-go/v3"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
)

type createPolicyStub struct {
	PolicyAdmin
	err    error
	writes int
}

func (s *createPolicyStub) InfoCannedPolicy(context.Context, string) ([]byte, error) {
	return nil, s.err
}
func (s *createPolicyStub) AddCannedPolicy(context.Context, string, []byte) error {
	s.writes++
	return nil
}

type createGroupStub struct {
	GroupAdmin
	err    error
	writes int
}

func (s *createGroupStub) GetGroupDescription(context.Context, string) (*madmin.GroupDesc, error) {
	return &madmin.GroupDesc{}, s.err
}
func (s *createGroupStub) UpdateGroupMembers(context.Context, madmin.GroupAddRemove) error {
	s.writes++
	return nil
}

func TestIAMCreateRequiresConsentAndNeverWritesObservedEntity(t *testing.T) {
	for _, kind := range []string{"policy", "group"} {
		for _, tc := range []struct {
			name    string
			consent bool
			err     error
			code    string
			writes  int
		}{
			{name: "missing consent", code: "ack", err: madmin.ErrorResponse{Code: "missing"}},
			{name: "accepted", consent: true, err: madmin.ErrorResponse{Code: "missing"}, writes: 1},
			{name: "observed existing", consent: true, code: "AlreadyExists"},
			{name: "existing without consent", code: "AlreadyExists"},
			{name: "read failure", consent: true, err: errors.New("PRIVATE"), code: "UpstreamError"},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				code := tc.code
				readErr := tc.err
				var native madmin.ErrorResponse
				if errors.As(readErr, &native) && native.Code == "missing" {
					if kind == "policy" {
						readErr = madmin.ErrorResponse{Code: "XMinioAdminNoSuchPolicy"}
					} else {
						readErr = madmin.ErrorResponse{Code: "XMinioAdminNoSuchGroup"}
					}
				}
				var err error
				var writes int
				if kind == "policy" {
					f := &createPolicyStub{err: readErr}
					err = CreatePolicy(context.Background(), f, CreatePolicyCommand{Name: "read", Document: json.RawMessage(`{"Version":"2012-10-17","Statement":[]}`), AcknowledgeNonAtomicCreate: tc.consent})
					writes = f.writes
					if code == "ack" {
						code = "PolicyCreateAcknowledgementRequired"
					}
				} else {
					f := &createGroupStub{err: readErr}
					err = CreateGroup(context.Background(), f, CreateGroupRequest{Name: "editors", Members: []string{"alice"}, AcknowledgeNonAtomicCreate: tc.consent})
					writes = f.writes
					if code == "ack" {
						code = "GroupCreateAcknowledgementRequired"
					}
				}
				if writes != tc.writes || (code == "" && err != nil) || (code != "" && (err == nil || apperror.Public(err).Code != code)) {
					t.Fatalf("writes=%d err=%v want=%s", writes, err, code)
				}
			})
		}
	}
}
