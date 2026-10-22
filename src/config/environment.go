package config

import (
	"errors"
	"slices"
	"strings"

	envconfig "github.com/caarlos0/env/v11"
)

// parseEnvironment supplies an isolated environment instead of consulting the
// process. Blank settings retain their initialized defaults, as before. Only
// explicitly listed fields bypass whitespace normalization (credentials and paths); env performs all primitive conversions without value expansion.
//
// Parse failures are retained by field so ordered domain validation can preserve
// the first reported variable. Library errors may contain secrets and never
// leave this boundary.
func parseEnvironment(target any, getenv func(string) string, prefix string, verbatim ...string) (map[string]bool, error) {
	options := envconfig.Options{Environment: map[string]string{}, Prefix: prefix}
	fields, err := envconfig.GetFieldParamsWithOptions(target, options)
	if err != nil {
		return nil, errors.New("invalid environment configuration")
	}
	for _, field := range fields {
		value := getenv(field.Key)
		if !slices.Contains(verbatim, field.OwnKey) {
			value = strings.TrimSpace(value)
		}
		options.Environment[field.Key] = value
	}
	invalid := map[string]bool{}
	if err := envconfig.ParseWithOptions(target, options); err != nil {
		var failures envconfig.AggregateError
		if !errors.As(err, &failures) {
			return nil, errors.New("invalid environment configuration")
		}
		for _, failure := range failures.Errors {
			var parseError envconfig.ParseError
			if !errors.As(failure, &parseError) {
				return nil, errors.New("invalid environment configuration")
			}
			invalid[parseError.Name] = true
		}
	}
	return invalid, nil
}
