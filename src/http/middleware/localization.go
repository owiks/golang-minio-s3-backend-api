package middleware

import (
	"net/http"
	"strings"

	"golang.org/x/text/language"

	"minio-web/api/src/http/requestctx"
	"minio-web/api/src/services/localization"
)

// ChooseLanguage bounds untrusted input before parsing BCP 47 tags. Only an
// explicitly requested supported base language is accepted, not a related one.
func ChooseLanguage(mode, accept string) string {
	if mode == "en" || mode == "ru" {
		return mode
	}
	if len(accept) > 1024 {
		return "en"
	}
	accept, valid := normalizeLanguageHeader(accept)
	if !valid {
		return "en"
	}
	tags, _, err := language.ParseAcceptLanguage(accept)
	if err != nil {
		return "en"
	}
	for _, tag := range tags {
		// Base infers a language for und-RU and und-Cyrl. Negotiation accepts
		// only an explicitly specified supported language, never that guess.
		base, _, _ := tag.Raw()
		if base.String() == "ru" || base.String() == "en" {
			return base.String()
		}
	}
	return "en"
}

// x/text accepts non-HTTP spellings and weights (including underscores, NaN and
// values above one). Reject those before locale negotiation.
func normalizeLanguageHeader(accept string) (string, bool) {
	ranges := strings.Split(accept, ",")
	for i, entry := range ranges {
		tag, weight, exists := strings.Cut(entry, ";")
		tag = strings.TrimSpace(tag)
		if !validLanguageRange(tag) {
			return "", false
		}
		if !exists {
			continue
		}
		name, value, exists := strings.Cut(strings.TrimSpace(weight), "=")
		if !exists || !strings.EqualFold(strings.TrimSpace(name), "q") {
			return "", false
		}
		value = strings.TrimSpace(value)
		if len(value) == 0 || len(value) > 5 || (value[0] != '0' && value[0] != '1') {
			return "", false
		}
		if len(value) > 1 {
			if value[1] != '.' {
				return "", false
			}
			for _, digit := range value[2:] {
				if digit < '0' || digit > '9' || (value[0] == '1' && digit != '0') {
					return "", false
				}
			}
		}
		ranges[i] = strings.TrimSpace(tag) + ";q=" + value
	}
	return strings.Join(ranges, ","), true
}

func validLanguageRange(tag string) bool {
	if tag == "" || tag == "*" {
		return true
	}
	for i, part := range strings.Split(tag, "-") {
		if len(part) == 0 || len(part) > 8 {
			return false
		}
		for _, c := range part {
			if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || i > 0 && c >= '0' && c <= '9' {
				continue
			}
			return false
		}
	}
	return true
}

func LocalizeRequest(r *http.Request, mode string, catalog *localization.Catalog) *http.Request {
	if catalog == nil {
		catalog = localization.Default()
	}
	values := r.Header.Values("Accept-Language")
	size := 0
	for _, value := range values {
		size += len(value) + 1
		if size > 1025 {
			values = nil
			break
		}
	}
	lang := ChooseLanguage(mode, strings.Join(values, ","))
	return r.WithContext(requestctx.WithLanguage(r.Context(), lang, mode != "en" && mode != "ru", catalog.For(lang)))
}
