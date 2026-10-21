package batch

import (
	"context"

	"github.com/minio/madmin-go/v3"
)

// Interfaces belong to the use cases that consume them. Only required
// operations are exposed; connection credentials and session state stay outside.
type CancelClient interface {
	CancelBatchJob(context.Context, string) error
}

type ListClient interface {
	ListBatchJobs(context.Context, *madmin.ListBatchJobsFilter) (madmin.ListBatchJobsResult, error)
}

type ReadBatchParametersClient interface {
	DescribeBatchJob(context.Context, string) (string, error)
}

type StartClient interface {
	StartBatchJob(context.Context, string) (madmin.BatchJobResult, error)
}

type StatusClient interface {
	BatchJobStatus(context.Context, string) (madmin.BatchJobStatus, error)
}
