package buckets

import "encoding/json"

type BucketDetails struct {
	Name       string       `json:"name"`
	Policy     *DetailField `json:"policy,omitempty"`
	Quota      *DetailField `json:"quota,omitempty"`
	Tags       *DetailField `json:"tags,omitempty"`
	Versioning *DetailField `json:"versioning,omitempty"`
	Locking    *DetailField `json:"locking,omitempty"`
	Lifecycle  *DetailField `json:"lifecycle,omitempty"`
	CORS       *DetailField `json:"cors,omitempty"`
	Encryption *DetailField `json:"encryption,omitempty"`
}

// SettingValue is the closed set of native setting representations. IAM policy
// JSON stays opaque, including future fields; its canonical revision is preserved.
type SettingValue interface{ bucketSetting() }
type PolicyDocument json.RawMessage

func (p PolicyDocument) MarshalJSON() ([]byte, error) { return json.RawMessage(p).MarshalJSON() }
func (PolicyDocument) bucketSetting()                 {}

type TagSetting map[string]string

func (TagSetting) bucketSetting()        {}
func (QuotaSetting) bucketSetting()      {}
func (VersioningSetting) bucketSetting() {}
func (LockingSetting) bucketSetting()    {}
