package buckets

import (
	"encoding/json"
	"strings"
	"testing"

	"minio-web/api/src/services/apperror"
)

func TestDetailsPreservePartialSelectionAndPolicyRevision(t *testing.T) {
	document := []byte(`{"Version":"2012-10-17","Statement":[],"FutureNumber":9007199254740993}`)
	decoder := json.NewDecoder(strings.NewReader(string(document)))
	decoder.UseNumber()
	var original any
	if err := decoder.Decode(&original); err != nil {
		t.Fatal(err)
	}
	typed := PolicyDocument(document)
	if SettingField(original, nil).Revision != SettingField(typed, nil).Revision {
		t.Fatal("policy revision changed after typing")
	}
	field := Detail(nil, apperror.New(403, "AccessDenied", "denied"))
	raw, err := json.Marshal(BucketDetails{Name: "bucket", Policy: &field})
	if err != nil || string(raw) != `{"name":"bucket","policy":{"error":"denied","errorCode":"AccessDenied"}}` {
		t.Fatalf("partial details changed: %s %v", raw, err)
	}
}
