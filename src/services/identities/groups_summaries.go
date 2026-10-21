package identities

import (
	"context"
	"sort"
	"time"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
)

func GroupSummaries(ctx context.Context, s GroupSummariesClient) ([]GroupSummary, error) {
	parent := ctx
	names, err := s.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	rows := make([]GroupSummary, len(names))
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for i, name := range names {
		rows[i].Name = name
		rows[i].Error = messages.IdentitiesTheDetailsWereNotRetrievedWithinThe
	}
	_ = ReadSummaryRows(ctx, len(names), func(i int) {
		rows[i].Name = names[i]
		group, err := s.GetGroupDescription(ctx, names[i])
		if err != nil {
			rows[i].Error = apperror.Public(err).Message
			return
		}
		rows[i].Error = nil
		rows[i].Details = &GroupSummaryDetails{Status: group.Status, Members: SortedUnique(group.Members), Policies: SortedUnique(SplitPolicies(group.Policy)), UpdatedAt: KnownTime(group.UpdatedAt)}
	})
	if err := parent.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}
