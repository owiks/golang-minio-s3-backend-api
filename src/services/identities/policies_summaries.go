package identities

import (
	"context"
	"sort"
	"time"

	"github.com/minio/madmin-go/v3"

	"minio-web/api/src/services/apperror"
)

func PolicySummaries(ctx context.Context, s PolicySummariesClient) ([]PolicySummary, error) {
	parent := ctx
	policies, err := s.ListCannedPolicies(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(policies))
	for name := range policies {
		names = append(names, name)
	}
	sort.Strings(names)
	rows := make([]PolicySummary, len(names))
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for i, name := range names {
		rows[i].Name = name
		rows[i].AssignmentsError = "Назначения не получены в отведённое время."
		rows[i].DatesError = "Даты не получены в отведённое время."
		value, err := SummarizePolicy(policies[name])
		rows[i].Definition = value
		if err != nil {
			rows[i].DefinitionError = apperror.Public(err).Message
		}
	}
	// Query only the displayed policies in bounded batches, not every identity in MinIO.
	for offset := 0; offset < len(names); offset += 32 {
		if ctx.Err() != nil {
			break
		}
		end := min(offset+32, len(names))
		entities, err := s.GetPolicyEntities(ctx, madmin.PolicyEntitiesQuery{Policy: names[offset:end]})
		for i := offset; i < end; i++ {
			if err != nil {
				rows[i].AssignmentsError = apperror.Public(err).Message
				continue
			}
			assignment := &PolicyAssignments{Users: []string{}, Groups: []string{}}
			for _, mapping := range entities.PolicyMappings {
				if mapping.Policy == rows[i].Name {
					assignment.Users = append(assignment.Users, mapping.Users...)
					assignment.Groups = append(assignment.Groups, mapping.Groups...)
				}
			}
			assignment.Users = SortedUnique(assignment.Users)
			assignment.Groups = SortedUnique(assignment.Groups)
			rows[i].AssignmentsError = ""
			rows[i].Assignments = assignment
		}
	}
	_ = ReadSummaryRows(ctx, len(rows), func(i int) {
		info, err := s.InfoCannedPolicyV2(ctx, names[i])
		if err != nil {
			rows[i].DatesError = apperror.Public(err).Message
			return
		}
		rows[i].DatesError = ""
		rows[i].CreatedAt = KnownTime(info.CreateDate)
		rows[i].UpdatedAt = KnownTime(info.UpdateDate)
	})
	if err := parent.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}
