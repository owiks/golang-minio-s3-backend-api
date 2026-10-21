package securetoken

import (
	"crypto/rand"
	"encoding/base64"
)

func New() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("secure random unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
