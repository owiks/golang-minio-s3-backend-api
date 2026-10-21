package batch

import (
	"context"

	"github.com/minio/madmin-go/v3"
)

func List(ctx context.Context, s ListClient) ([]Job, error) {
	jobs, err := s.ListBatchJobs(ctx, &madmin.ListBatchJobsFilter{})
	if err != nil {
		return nil, err
	}
	if jobs.Jobs == nil {
		jobs.Jobs = []madmin.BatchJobResult{}
	}
	out := make([]Job, len(jobs.Jobs))
	for i, job := range jobs.Jobs {
		out[i] = jobResult(job)
	}
	return out, nil
}
func Start(ctx context.Context, s StartClient, input BatchInput) (Job, error) {
	doc, err := BatchDocument(input)
	if err != nil {
		return Job{}, err
	}
	value, err := s.StartBatchJob(ctx, doc)
	return jobResult(value), err
}
func Status(ctx context.Context, s StatusClient, id string) (BatchMetric, error) {
	v, err := s.BatchJobStatus(ctx, id)
	if err != nil {
		return BatchMetric{}, err
	}
	return SafeBatchMetric(v.LastMetric), nil
}
func Cancel(ctx context.Context, s CancelClient, id string) error {
	return s.CancelBatchJob(ctx, id)
}
