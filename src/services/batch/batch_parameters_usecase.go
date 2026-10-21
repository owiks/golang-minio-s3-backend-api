package batch

import (
	"context"
)

func ReadBatchParameters(ctx context.Context, s ReadBatchParametersClient, id string) (BatchParameters, error) {
	raw, err := s.DescribeBatchJob(ctx, id)
	if err != nil {
		return BatchParameters{}, err
	}
	view, err := SafeBatchParameters(raw)
	if err != nil {
		return BatchParameters{}, err
	}
	return view, nil
}
