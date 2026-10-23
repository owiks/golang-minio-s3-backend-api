package authstate

import (
	"encoding/hex"
	"unicode/utf8"
)

func validID(id string) bool {
	if len(id) < 1 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if r != '-' && r != '_' && (r < '0' || r > '9') && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}
func validHash(s string) bool { raw, err := hex.DecodeString(s); return err == nil && len(raw) == 32 }
func ValidConnection(c ConnectionData) bool {
	return validID(c.ID) && validHash(c.Owner) && validHash(c.Actor) && c.Revision > 0 && c.SecretVersion > 0 && !c.CreatedAt.IsZero() && len(c.Endpoint) <= 2048 && len(c.Region) <= 128 && (!c.Active || c.Cipher.KeyID != "" && len(c.Cipher.KeyID) <= 64 && len(c.Cipher.Data) >= 28 && len(c.Cipher.Data) <= 131072)
}
func ValidAccess(a AccessData) bool {
	if a.ParentID != "" && (!validID(a.ParentID) || a.ParentID == a.ID || a.Mode != "bearer" || !a.Renewable) || a.Mode == "cookie" && a.Renewable {
		return false
	}
	return validID(a.ID) && validID(a.ConnectionID) && validHash(a.Owner) && a.Revision > 0 && a.Generation > 0 && !a.CreatedAt.IsZero() && (a.Mode == "bearer" || !a.ExpiresAt.IsZero()) && utf8.ValidString(a.Name) && len(a.Name) <= 128 && validHash(a.Digest) && a.Policy.Valid() && (a.Mode == "cookie" && validHash(a.CSRFDigest) || a.Mode == "bearer") && (a.PreviousDigest == "" || validHash(a.PreviousDigest) && !a.PreviousUntil.IsZero())
}

func validSecrets(s Secrets) bool {
	return s.AccessKey != "" && s.SecretKey != "" && len(s.AccessKey) <= 256 && len(s.SecretKey) <= 4096 && len(s.SessionToken) <= 16384 && utf8.ValidString(s.AccessKey) && utf8.ValidString(s.SecretKey) && utf8.ValidString(s.SessionToken)
}
