package presentation

import (
	"minio-web/api/src/services/identities"
	"minio-web/api/src/services/localization"
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
