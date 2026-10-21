package validation

import (
	"strings"
	"unicode"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

func ValidName(name string) bool {
	return len(name) > 0 && len(name) <= 128 && strings.TrimSpace(name) == name && !strings.ContainsAny(name, "/,\\") && !strings.ContainsFunc(name, unicode.IsControl)
}

func ValidateMembers(members []string) error {
	if len(members) == 0 || len(members) > 100 {
		return apperror.New(400, "InvalidMembers", messages.ValidationInvalidMembers)
	}
	seen := map[string]bool{}
	for _, name := range members {
		if !ValidName(name) || seen[name] {
			return apperror.New(400, "InvalidMembers", messages.ValidationInvalidMembersManagement)
		}
		seen[name] = true
	}
	return nil
}
