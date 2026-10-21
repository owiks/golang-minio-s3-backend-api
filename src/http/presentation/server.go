package presentation

import (
	"minio-web/api/src/services/localization"
	"minio-web/api/src/services/server"
)

type ConfigField struct {
	server.ConfigField
	Label string `json:"label"`
}

func presentServerConfigField(localizer *localization.Localizer, v server.ConfigField) ConfigField {
	return ConfigField{ConfigField: v,
		Label: localizer.Render(v.Label),
	}
}

type PoolsView struct {
	server.PoolsView
	Reason         string `json:"reason,omitempty"`
	RebalanceError string `json:"rebalanceError,omitempty"`
}

func presentServerPoolsView(localizer *localization.Localizer, v server.PoolsView) PoolsView {
	return PoolsView{PoolsView: v,
		Reason:         localizer.Render(v.Reason),
		RebalanceError: localizer.Render(v.RebalanceError),
	}
}

type ServerConfigView struct {
	server.ServerConfigView
	Fields       []ConfigField `json:"fields"`
	HistoryError string        `json:"historyError,omitempty"`
}

func presentServerServerConfigView(localizer *localization.Localizer, v server.ServerConfigView) ServerConfigView {
	return ServerConfigView{ServerConfigView: v,
		Fields:       mapSlice(v.Fields, func(x server.ConfigField) ConfigField { return presentServerConfigField(localizer, x) }),
		HistoryError: localizer.Render(v.HistoryError),
	}
}

type ServerSnapshot struct {
	server.ServerSnapshot
	UsageError string `json:"usageError,omitempty"`
}

func presentServerServerSnapshot(localizer *localization.Localizer, v server.ServerSnapshot) ServerSnapshot {
	return ServerSnapshot{ServerSnapshot: v,
		UsageError: localizer.Render(v.UsageError),
	}
}
