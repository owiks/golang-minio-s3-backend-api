package requestctx

import (
	"context"

	"minio-web/api/src/services/localization"
)

type languageKey struct{}
type languageState struct {
	name      string
	auto      bool
	localizer *localization.Localizer
}

func WithLanguage(ctx context.Context, name string, auto bool, localizer *localization.Localizer) context.Context {
	return context.WithValue(ctx, languageKey{}, languageState{name, auto, localizer})
}
func Language(ctx context.Context) string {
	if state, ok := ctx.Value(languageKey{}).(languageState); ok {
		return state.name
	}
	return "en"
}
func AutomaticLanguage(ctx context.Context) bool {
	state, _ := ctx.Value(languageKey{}).(languageState)
	return state.auto
}
func Localizer(ctx context.Context) *localization.Localizer {
	if state, ok := ctx.Value(languageKey{}).(languageState); ok && state.localizer != nil {
		return state.localizer
	}
	return localization.Default().For("en")
}
