// Package capabilities describes evidence for individual storage operations.
// It never grants access or substitutes for native write preconditions.
package capabilities

import (
	"context"
	"strings"
	"time"
)

type Identifier string

const (
	ObjectAttributes    Identifier = "object.attributes"
	MultipartList       Identifier = "multipart.list"
	MultipartCreate     Identifier = "multipart.create"
	MultipartPartPut    Identifier = "multipart.part.put"
	MultipartPartsList  Identifier = "multipart.parts.list"
	MultipartComplete   Identifier = "multipart.complete"
	MultipartAbort      Identifier = "multipart.abort"
	ConditionalPut      Identifier = "object.put.conditional"
	ConditionalComplete Identifier = "multipart.complete.conditional"
	SelectCSV           Identifier = "select.csv"
	SelectJSON          Identifier = "select.json"
	DirectPartUpload    Identifier = "upload.direct.part"
	BrowserCORS         Identifier = "upload.direct.cors"
)

type Status string

const (
	Supported    Status = "supported"
	Unsupported  Status = "unsupported"
	AccessDenied Status = "access_denied"
	Unknown      Status = "unknown"
)

type AccessStatus string

const (
	AccessAllowed   AccessStatus = "allowed"
	AccessForbidden AccessStatus = "denied"
	AccessUnknown   AccessStatus = "unknown"
)

type Scope struct {
	Bucket    string `json:"bucket,omitempty"`
	Key       string `json:"key,omitempty"`
	VersionID string `json:"versionId,omitempty"`
}

// Results apply only to the authenticated session, endpoint, IAM revision and
// exact resource scope used to obtain them. Success is evidence, not permission.
type Result struct {
	ID     Identifier `json:"id"`
	Status Status     `json:"status"`
	// Access describes the observed response at CheckedAt, never a grant for a
	// subsequent operation. Confirmed configuration leaves access unknown.
	Access    AccessStatus `json:"access"`
	Scope     Scope        `json:"scope"`
	Evidence  string       `json:"evidence"`
	Reason    string       `json:"reason"`
	CheckedAt time.Time    `json:"checkedAt"`
	ExpiresAt time.Time    `json:"expiresAt"`
}

type Snapshot struct {
	Capabilities []Result `json:"capabilities"`
}
type Request struct {
	Scope   Scope
	Feature Identifier
}

// Cache is session-bound by its infrastructure owner; business code cannot
// choose another actor, endpoint or IAM revision. Resolve coalesces safe reads.
type Cache interface {
	Resolve(context.Context, Identifier, Scope, func(context.Context) (Result, error)) (Result, error)
}

// Observation contains the outcome of an actual user-requested upstream
// operation after response validation. Local signing/validation is not evidence.
type Observation struct {
	ID    Identifier
	Scope Scope
	Err   error
}
type Observer interface {
	Observe(context.Context, Observation)
}

// ChecksumID keeps algorithm and checksum type distinct. Unknown combinations
// are not introduced into the capability cache.
func ChecksumID(algorithm, mode string) Identifier {
	a := strings.ToLower(strings.TrimSpace(algorithm))
	m := strings.ToLower(strings.TrimSpace(mode))
	switch a {
	case "md5", "crc32", "crc32c", "sha1", "sha256", "crc64nvme":
	default:
		return ""
	}
	switch m {
	case "full_object", "composite":
	default:
		return ""
	}
	return Identifier("checksum." + a + "." + m)
}

func Identifiers() []Identifier {
	ids := []Identifier{ObjectAttributes, MultipartList, MultipartCreate, MultipartPartPut,
		MultipartPartsList, MultipartComplete, MultipartAbort, ConditionalPut,
		ConditionalComplete, SelectCSV, SelectJSON, DirectPartUpload, BrowserCORS}
	for _, algorithm := range []string{"md5", "crc32", "crc32c", "sha1", "sha256", "crc64nvme"} {
		for _, mode := range []string{"full_object", "composite"} {
			ids = append(ids, ChecksumID(algorithm, mode))
		}
	}
	return ids
}

func Known(id Identifier) bool {
	for _, known := range Identifiers() {
		if known == id {
			return true
		}
	}
	return false
}

func TTL(status Status) time.Duration {
	switch status {
	case Supported, Unsupported:
		return 5 * time.Minute
	case AccessDenied:
		return 30 * time.Second
	default:
		return 10 * time.Second
	}
}
