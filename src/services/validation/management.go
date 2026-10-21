package validation

import (
	"strings"
	"unicode"

	"minio-web/api/src/services/apperror"
)

func ValidName(name string) bool {
	return len(name) > 0 && len(name) <= 128 && strings.TrimSpace(name) == name && !strings.ContainsAny(name, "/,\\") && !strings.ContainsFunc(name, unicode.IsControl)
}

func ValidateMembers(members []string) error {
	if len(members) == 0 || len(members) > 100 {
		return apperror.New(400, "InvalidMembers", "Выберите от 1 до 100 участников.")
	}
	seen := map[string]bool{}
	for _, name := range members {
		if !ValidName(name) || seen[name] {
			return apperror.New(400, "InvalidMembers", "Участники должны иметь корректные, неповторяющиеся имена.")
		}
		seen[name] = true
	}
	return nil
}
