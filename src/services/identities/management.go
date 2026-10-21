package identities

import (
	"errors"
	"strings"

	"github.com/minio/madmin-go/v3"
)

func Missing(err error, code string) bool {
	var e madmin.ErrorResponse
	if err == nil {
		return false
	}
	if !errors.As(err, &e) {
		return false
	}
	return e.Code == code
}

func SplitPolicies(value string) []string {
	if value == "" {
		return []string{}
	}
	return strings.Split(value, ",")
}
