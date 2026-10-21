package storage

import (
	"context"
	"net/http"

	"github.com/minio/madmin-go/v3"
)

// Commands keep the native SDK payloads and validation, with one attempt per call.

func (c *AdminClient) AddUser(ctx context.Context, name, secret string) error {
	return adminChange(ctx, func(ctx context.Context) error {
		return c.AdminClient.AddUser(ctx, name, secret)
	})
}

func (c *AdminClient) SetUserStatus(ctx context.Context, name string, status madmin.AccountStatus) error {
	return adminChange(ctx, func(ctx context.Context) error {
		return c.AdminClient.SetUserStatus(ctx, name, status)
	})
}

func (c *AdminClient) RemoveUser(ctx context.Context, name string) error {
	return adminChange(ctx, func(ctx context.Context) error {
		return c.AdminClient.RemoveUser(ctx, name)
	})
}

func (c *AdminClient) UpdateGroupMembers(ctx context.Context, group madmin.GroupAddRemove) error {
	return adminChange(ctx, func(ctx context.Context) error {
		return c.AdminClient.UpdateGroupMembers(ctx, group)
	})
}

func (c *AdminClient) SetGroupStatus(ctx context.Context, name string, status madmin.GroupStatus) error {
	return adminChange(ctx, func(ctx context.Context) error {
		return c.AdminClient.SetGroupStatus(ctx, name, status)
	})
}

func (c *AdminClient) AddCannedPolicy(ctx context.Context, name string, document []byte) error {
	return adminChange(ctx, func(ctx context.Context) error {
		return c.AdminClient.AddCannedPolicy(ctx, name, document)
	})
}

func (c *AdminClient) RemoveCannedPolicy(ctx context.Context, name string) error {
	return adminChange(ctx, func(ctx context.Context) error {
		return c.AdminClient.RemoveCannedPolicy(ctx, name)
	})
}

func (c *AdminClient) AttachPolicy(ctx context.Context, request madmin.PolicyAssociationReq) (madmin.PolicyAssociationResp, error) {
	return adminCommand(ctx, func(ctx context.Context) (madmin.PolicyAssociationResp, error) {
		return c.AdminClient.AttachPolicy(ctx, request)
	})
}

func (c *AdminClient) DetachPolicy(ctx context.Context, request madmin.PolicyAssociationReq) (madmin.PolicyAssociationResp, error) {
	return adminCommand(ctx, func(ctx context.Context) (madmin.PolicyAssociationResp, error) {
		return c.AdminClient.DetachPolicy(ctx, request)
	})
}

func (c *AdminClient) AddServiceAccount(ctx context.Context, request madmin.AddServiceAccountReq) (madmin.Credentials, error) {
	return adminCommand(ctx, func(ctx context.Context) (madmin.Credentials, error) {
		return c.AdminClient.AddServiceAccount(ctx, request)
	})
}

func (c *AdminClient) UpdateServiceAccount(ctx context.Context, name string, request madmin.UpdateServiceAccountReq) error {
	return adminChange(ctx, func(ctx context.Context) error {
		return c.AdminClient.UpdateServiceAccount(ctx, name, request)
	})
}

func (c *AdminClient) DeleteServiceAccount(ctx context.Context, name string) error {
	return adminChange(ctx, func(ctx context.Context) error {
		return c.AdminClient.DeleteServiceAccount(ctx, name)
	})
}

func (c *AdminClient) SetBucketQuota(ctx context.Context, bucket string, quota *madmin.BucketQuota) error {
	return adminChange(ctx, func(ctx context.Context) error {
		return c.AdminClient.SetBucketQuota(ctx, bucket, quota)
	})
}

func (c *AdminClient) SetConfigKV(ctx context.Context, values string) (bool, error) {
	return adminCommand(ctx, func(ctx context.Context) (bool, error) {
		return c.AdminClient.SetConfigKV(ctx, values)
	})
}

func (c *AdminClient) AddTier(ctx context.Context, config *madmin.TierConfig) error {
	return adminChange(ctx, func(ctx context.Context) error {
		return c.AdminClient.AddTier(ctx, config)
	})
}

func (c *AdminClient) EditTier(ctx context.Context, name string, credentials madmin.TierCreds) error {
	return adminChange(ctx, func(ctx context.Context) error {
		return c.AdminClient.EditTier(ctx, name, credentials)
	})
}

func (c *AdminClient) RemoveTier(ctx context.Context, name string) error {
	return adminChange(ctx, func(ctx context.Context) error {
		return c.AdminClient.RemoveTier(ctx, name)
	})
}

func (c *AdminClient) VerifyTier(ctx context.Context, name string) error {
	return adminChange(ctx, func(ctx context.Context) error {
		return c.AdminClient.VerifyTier(ctx, name)
	})
}

func (c *AdminClient) StartBatchJob(ctx context.Context, document string) (madmin.BatchJobResult, error) {
	return adminCommand(ctx, func(ctx context.Context) (madmin.BatchJobResult, error) {
		return c.AdminClient.StartBatchJob(ctx, document)
	})
}

func (c *AdminClient) CancelBatchJob(ctx context.Context, id string) error {
	return adminChange(ctx, func(ctx context.Context) error {
		return c.AdminClient.CancelBatchJob(ctx, id)
	})
}

func (c *AdminClient) RebalanceStart(ctx context.Context) (string, error) {
	return adminCommand(ctx, func(ctx context.Context) (string, error) {
		return c.AdminClient.RebalanceStart(ctx)
	})
}

func (c *AdminClient) RebalanceStop(ctx context.Context) error {
	return adminChange(ctx, func(ctx context.Context) error {
		return c.AdminClient.RebalanceStop(ctx)
	})
}

func (c *AdminClient) DecommissionPool(ctx context.Context, pool string) error {
	return adminChange(ctx, func(ctx context.Context) error {
		return c.AdminClient.DecommissionPool(ctx, pool)
	})
}

func (c *AdminClient) CancelDecommissionPool(ctx context.Context, pool string) error {
	return adminChange(ctx, func(ctx context.Context) error {
		return c.AdminClient.CancelDecommissionPool(ctx, pool)
	})
}

func (c *AdminClient) ExecuteMethod(ctx context.Context, method string, request madmin.RequestData) (*http.Response, error) {
	if method == http.MethodGet || method == http.MethodHead {
		return c.AdminClient.ExecuteMethod(ctx, method, request)
	}
	return adminCommand(ctx, func(ctx context.Context) (*http.Response, error) {
		return c.AdminClient.ExecuteMethod(ctx, method, request)
	})
}
