package buckets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
	"unicode"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/cors"
	"github.com/minio/minio-go/v7/pkg/sse"

	"minio-web/api/src/services/apperror"
)

type CorsRule struct {
	ID      string   `json:"id"`
	Origins []string `json:"origins"`
	Methods []string `json:"methods"`
	Headers []string `json:"headers"`
	Expose  []string `json:"expose"`
	MaxAge  int      `json:"maxAge"`
}
type BrowserSetting struct {
	Rules     []CorsRule `json:"rules,omitempty"`
	Algorithm string     `json:"algorithm,omitempty"`
	KeyID     string     `json:"keyId,omitempty"`
	Writable  bool       `json:"writable"`
	Warning   string     `json:"warning,omitempty"`
}
type BrowserSettingState struct {
	Value      BrowserSetting     `json:"-"`
	Revision   string             `json:"-"`
	Cors       *cors.Config       `json:"-"`
	Encryption *sse.Configuration `json:"-"`
}

func ReadBrowserSetting(ctx context.Context, s ReadBrowserSettingsClient, bucket, kind string) (BrowserSettingState, error) {
	var raw []byte
	state := BrowserSettingState{Value: BrowserSetting{Writable: true, Rules: []CorsRule{}}}
	var original any
	if kind == "cors" {
		read, err := s.ReadCORSConfig(ctx, bucket)
		cfg := read.Value
		raw = read.Raw
		if minio.ToErrorResponse(err).Code == "NoSuchCORSConfiguration" {
			err = nil
			cfg = cors.NewConfig([]cors.Rule{})
			raw = nil
		}
		if err != nil {
			return state, err
		}
		if cfg == nil {
			cfg = cors.NewConfig([]cors.Rule{})
			raw = nil
		}
		state.Cors = cfg
		original = cfg
		if cfg != nil {
			for _, r := range cfg.CORSRules {
				state.Value.Rules = append(state.Value.Rules, CorsRule{ID: r.ID, Origins: NonNil(r.AllowedOrigin), Methods: NonNil(r.AllowedMethod), Headers: NonNil(r.AllowedHeader), Expose: NonNil(r.ExposeHeader), MaxAge: r.MaxAgeSeconds})
			}
		}
	} else {
		read, err := s.ReadEncryptionConfig(ctx, bucket)
		cfg := read.Value
		raw = read.Raw
		if minio.ToErrorResponse(err).Code == "ServerSideEncryptionConfigurationNotFoundError" {
			err = nil
			cfg = nil
			raw = nil
		}
		if err != nil {
			return state, err
		}
		state.Encryption = cfg
		original = cfg
		if cfg != nil && len(cfg.Rules) > 0 {
			state.Value.Algorithm = cfg.Rules[0].Apply.SSEAlgorithm
			state.Value.KeyID = cfg.Rules[0].Apply.KmsMasterKeyID
		}
		if cfg != nil && len(cfg.Rules) != 1 {
			state.Value.Writable = false
		}
	}
	hash := sha256.Sum256(raw)
	state.Revision = hex.EncodeToString(hash[:])
	if len(raw) > 0 && !LifecycleRoundTripSafe(raw, original) {
		state.Value.Writable = false
	}
	if !state.Value.Writable {
		state.Value.Warning = "Настройка содержит параметры, которые текущий SDK не может сохранить без потерь. Доступен только просмотр."
	}
	return state, nil
}
func NonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
func BrowserSettingField(state BrowserSettingState, err error) DetailField {
	if err != nil {
		return Detail(nil, err)
	}
	return DetailField{Value: state.Value, Revision: state.Revision}
}
func ValidateCorsRule(r CorsRule) error {
	invalid := func() error {
		return apperror.New(400, "InvalidCORS", "Проверьте CORS: укажите origins, методы GET/HEAD/PUT/POST/DELETE, корректные заголовки и неотрицательный Max-Age.")
	}
	if len(r.ID) > 255 || strings.ContainsFunc(r.ID, unicode.IsControl) || len(r.Origins) == 0 || len(r.Origins) > 100 || len(r.Methods) == 0 || len(r.Methods) > 5 || len(r.Headers) > 100 || len(r.Expose) > 100 || r.MaxAge < 0 || r.MaxAge > 2147483647 {
		return invalid()
	}
	for _, origin := range r.Origins {
		if origin == "*" {
			continue
		}
		if len(origin) > 2048 || strings.Count(origin, "*") > 1 {
			return invalid()
		}
		u, err := url.Parse(strings.ReplaceAll(origin, "*", "cors-wildcard"))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(origin, "\r\n\t ") {
			return invalid()
		}
	}
	seen := map[string]bool{}
	for _, m := range r.Methods {
		if seen[m] || (m != "GET" && m != "HEAD" && m != "PUT" && m != "POST" && m != "DELETE") {
			return invalid()
		}
		seen[m] = true
	}
	for group, headers := range [][]string{r.Headers, r.Expose} {
		for _, h := range headers {
			if h == "" || len(h) > 256 || strings.Count(h, "*") > 1 {
				return invalid()
			}
			for _, c := range h {
				if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'+-.^_`|~", c) || group == 0 && c == '*') {
					return invalid()
				}
			}
		}
	}
	return nil
}
