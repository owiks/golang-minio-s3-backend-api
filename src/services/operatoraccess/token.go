// Package operatoraccess verifies the process operator credential independently
// of user connections, cookies and S3 permissions.
package operatoraccess

import (
	"crypto/sha256"
	"crypto/subtle"
	"strings"
)

// ValidToken accepts a bounded printable bearer secret. Length cannot establish
// entropy; operators must generate the value with a cryptographic random source.
func ValidToken(value string) bool {
	if len(value) < 32 || len(value) > 512 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~+/=", c)) {
			return false
		}
	}
	return true
}

// Match never accepts a user cookie as fallback and requires one Authorization
// field. Comparing fixed-size digests avoids leaking the configured length.
func Match(expected string, authorization []string) bool {
	if !ValidToken(expected) || len(authorization) != 1 {
		return false
	}
	value := authorization[0]
	scheme, token, found := strings.Cut(value, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || !ValidToken(token) {
		return false
	}
	want, got := sha256.Sum256([]byte(expected)), sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(want[:], got[:]) == 1
}
