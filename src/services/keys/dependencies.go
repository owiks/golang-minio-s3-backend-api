package keys

import (
	"context"

	"github.com/minio/madmin-go/v3"
)

// Interfaces belong to the use cases that consume them. Only required
// operations are exposed; connection credentials and session state stay outside.
type CreateKeyClient interface {
	AddServiceAccount(context.Context, madmin.AddServiceAccountReq) (madmin.Credentials, error)
}

type DeleteKeyClient interface {
	ActorName() string
	DeleteServiceAccount(context.Context, string) error
}

type EnsureMutableClient interface {
	ActorName() string
}

type ServiceKeyReader interface {
	ActorName() string
	InfoServiceAccount(context.Context, string) (madmin.InfoServiceAccountResp, error)
}

type KeyDetailClient interface {
	ServiceKeyReader
	TemporaryAccountInfo(context.Context, string) (madmin.TemporaryAccountInfoResp, error)
}

type ListKeysClient interface {
	ActorName() string
	ListAccessKeysBulk(context.Context, []string, madmin.ListAccessKeysOpts) (map[string]madmin.ListAccessKeysResp, error)
	ListServiceAccounts(context.Context, string) (madmin.ListServiceAccountsResp, error)
}

type ChangeKeyClient interface {
	ServiceKeyReader
	UpdateServiceAccount(context.Context, string, madmin.UpdateServiceAccountReq) error
}
