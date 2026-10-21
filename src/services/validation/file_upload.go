package validation

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func ValidFileKey(key string) bool {
	if key == "" || len(key) > 1024 || !utf8.ValidString(key) || strings.HasSuffix(key, "/") || strings.ContainsAny(key, "\\") || strings.ContainsFunc(key, unicode.IsControl) {
		return false
	}
	for _, part := range strings.Split(key, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}
