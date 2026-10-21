package objects

import (
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

type ObjectPage struct {
	Entries   []Entry `json:"entries"`
	NextToken string  `json:"nextToken"`
}
type DeletionInfo struct {
	Revision string    `json:"revision"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}
type Content struct {
	Text     string       `json:"text"`
	Revision string       `json:"revision"`
	Writable bool         `json:"writable"`
	Reason   messages.Ref `json:"reason"`
}
type PropertiesUpdated struct {
	OK                bool `json:"ok"`
	MetadataRewritten bool `json:"metadataRewritten"`
}

type ChecksumComparison struct {
	Algorithm string `json:"algorithm"`
	Source    string `json:"source"`
	Expected  string `json:"expected"`
	Computed  string `json:"computed"`
}
type ChecksumResult struct {
	SHA256      string               `json:"sha256"`
	Bytes       int64                `json:"bytes"`
	Version     string               `json:"version"`
	Status      string               `json:"status"`
	Comparisons []ChecksumComparison `json:"comparisons"`
}
