package presentation

import (
	"github.com/owiks/golang-minio-s3-backend-api/src/services/identities"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/localization"
)

type GroupSummary struct {
	identities.GroupSummary
	Error string `json:"error,omitempty"`
}

func presentIdentitiesGroupSummary(localizer *localization.Localizer, v identities.GroupSummary) GroupSummary {
	return GroupSummary{GroupSummary: v,
		Error: localizer.Render(v.Error),
	}
}

type PolicySummary struct {
	identities.PolicySummary
	DefinitionError  string `json:"definitionError,omitempty"`
	AssignmentsError string `json:"assignmentsError,omitempty"`
	DatesError       string `json:"datesError,omitempty"`
}

func presentIdentitiesPolicySummary(localizer *localization.Localizer, v identities.PolicySummary) PolicySummary {
	return PolicySummary{PolicySummary: v,
		DefinitionError:  localizer.Render(v.DefinitionError),
		AssignmentsError: localizer.Render(v.AssignmentsError),
		DatesError:       localizer.Render(v.DatesError),
	}
}
