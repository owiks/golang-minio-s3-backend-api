package revision

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// IAM lists are sets. Compare canonical content, not server JSON map/set order.
func Of(raw []byte) string {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&value) == nil {
		if data, err := json.Marshal(canonicalJSON(value)); err == nil {
			raw = data
		}
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func canonicalJSON(value any) any {
	switch item := value.(type) {
	case map[string]any:
		for key, value := range item {
			item[key] = canonicalJSON(value)
		}
	case []any:
		for i, value := range item {
			item[i] = canonicalJSON(value)
		}
		sort.Slice(item, func(i, j int) bool {
			a, _ := json.Marshal(item[i])
			b, _ := json.Marshal(item[j])
			return bytes.Compare(a, b) < 0
		})
	}
	return value
}
