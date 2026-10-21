package server

import (
	"context"
	"maps"
	"sort"
	"strconv"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
	validationsvc "minio-web/api/src/services/validation"
)

type ConfigChangeRequest struct {
	Revision  string            `json:"revision"`
	Values    map[string]string `json:"values"`
	RestoreID string            `json:"restoreId,omitempty"`
}

type ConfigChangeResult struct {
	OK              bool `json:"ok"`
	RestartRequired bool `json:"restartRequired"`
}

func LoadConfig(ctx context.Context, s LoadConfigClient, sub string) (ServerConfigView, error) {
	current, e := ReadServerConfig(ctx, s, sub)
	if e != nil {
		return ServerConfigView{}, e
	}
	entries, e := s.ListConfigHistoryKV(ctx, 100)
	if e != nil {
		current.HistoryError = messages.ServerMinIOHistoryIsUnavailable
	} else {
		current.History = ConfigHistory(entries, sub)
	}
	return current, nil
}

// SaveConfig owns authorization and the current snapshot; callers supply only
// the requested change, never trusted field definitions or server state.
func SaveConfig(ctx context.Context, s SaveConfigClient, sub string, body ConfigChangeRequest) (ConfigChangeResult, error) {
	if err := validateConfigSubsystem(sub); err != nil {
		return ConfigChangeResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return ConfigChangeResult{}, err
	}
	body.Values = maps.Clone(body.Values)
	if err := RequireServerAction(ctx, s, "admin:ConfigUpdate"); err != nil {
		return ConfigChangeResult{}, err
	}
	current, err := ReadServerConfig(ctx, s, sub)
	if err != nil {
		return ConfigChangeResult{}, err
	}
	if body.Revision != current.Revision {
		return ConfigChangeResult{}, apperror.New(409, "ServerConfigChanged", messages.ServerServerConfigChanged)
	}
	if body.RestoreID != "" {
		if len(body.Values) != 0 || !validationsvc.ValidName(body.RestoreID) {
			return ConfigChangeResult{}, apperror.New(400, "InvalidConfigHistory", messages.ServerInvalidConfigHistory)
		}
		entries, e := s.ListConfigHistoryKV(ctx, 100)
		if e != nil {
			return ConfigChangeResult{}, ServerOperationError(e)
		}
		for _, h := range ConfigHistory(entries, sub) {
			if h.ID == body.RestoreID {
				body.Values = h.Values
				break
			}
		}
	}
	if len(body.Values) == 0 || len(body.Values) > 30 {
		return ConfigChangeResult{}, apperror.New(400, "InvalidConfigValues", messages.ServerInvalidConfigValues)
	}
	keys := []string{}
	for key, value := range body.Values {
		found := false
		for _, f := range current.Fields {
			if f.Key == key {
				found = true
				if f.Env != "" {
					return ConfigChangeResult{}, apperror.New(409, "ConfigEnvironmentOverride", messages.ServerConfigEnvironmentOverride)
				}
				if !ConfigValueValid(f, value) {
					return ConfigChangeResult{}, apperror.New(400, "InvalidConfigValue", messages.ServerInvalidConfigValue.With(messages.String("Name", key)))
				}
			}
		}
		if !found {
			return ConfigChangeResult{}, apperror.New(400, "InvalidConfigKey", messages.ServerInvalidConfigKey)
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	line := sub
	for _, key := range keys {
		line += " " + key + "=" + strconv.Quote(body.Values[key])
	}
	if err := ctx.Err(); err != nil {
		return ConfigChangeResult{}, err
	}
	restart, e := s.SetConfigKV(ctx, line)
	if e != nil {
		return ConfigChangeResult{}, ServerOperationError(e)
	}
	return ConfigChangeResult{OK: true, RestartRequired: restart}, nil
}
