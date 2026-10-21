package server

import (
	"context"

	"github.com/minio/madmin-go/v3"

	"minio-web/api/src/services/iam"
	"minio-web/api/src/services/storage"
)

// Interfaces belong to the use cases that consume them. Only required
// operations are exposed; connection credentials and session state stay outside.
type ReadDiagnosticsClient interface {
	OpenDiagnosticTrace(context.Context, bool) (storage.DiagnosticStream[madmin.ServiceTraceInfo], error)
	OpenDiagnosticLogs(context.Context, int) (storage.DiagnosticStream[madmin.LogInfo], error)
}

type ChangePoolsClient interface {
	ActionPolicySnapshot() *iam.Policy
	ActorName() string
	CancelDecommissionPool(context.Context, string) error
	DecommissionPool(context.Context, string) error
	ListPoolsStatus(context.Context) ([]madmin.PoolStatus, error)
	RebalanceStart(context.Context) (string, error)
	RebalanceStatus(context.Context) (madmin.RebalanceStatus, error)
	RebalanceStop(context.Context) error
	RefreshActionPolicy(context.Context) *iam.ActionCheckIssue
	ServerInfo(context.Context, ...func(*madmin.ServerInfoOpts)) (madmin.InfoMessage, error)
}

type LoadConfigClient interface {
	ReadServerConfigClient
	ListConfigHistoryKV(context.Context, int) ([]madmin.ConfigHistoryEntry, error)
}

type ReadLiveMetricsClient interface {
	ReadS3Metrics(context.Context) ([]byte, error)
}

type ReadPoolsClient interface {
	ListPoolsStatus(context.Context) ([]madmin.PoolStatus, error)
	RebalanceStatus(context.Context) (madmin.RebalanceStatus, error)
	ServerInfo(context.Context, ...func(*madmin.ServerInfoOpts)) (madmin.InfoMessage, error)
}

type ReadServerConfigClient interface {
	GetConfigKVWithOptions(context.Context, string, madmin.KVOptions) ([]byte, error)
}

type ReadServerInfoClient interface {
	DataUsageInfo(context.Context) (madmin.DataUsageInfo, error)
	ServerInfo(context.Context, ...func(*madmin.ServerInfoOpts)) (madmin.InfoMessage, error)
}

type RequireServerActionClient interface {
	ActionPolicySnapshot() *iam.Policy
	ActorName() string
	RefreshActionPolicy(context.Context) *iam.ActionCheckIssue
}

type SaveConfigClient interface {
	RequireServerActionClient
	ReadServerConfigClient
	ListConfigHistoryKV(context.Context, int) ([]madmin.ConfigHistoryEntry, error)
	SetConfigKV(context.Context, string) (bool, error)
}
