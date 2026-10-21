package buckets

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/minio/minio-go/v7"
)

func ReadPolicy(ctx context.Context, s ReadPolicyClient, bucket string) (PolicyDocument, error) {
	raw, err := s.GetBucketPolicy(ctx, bucket)
	if err != nil || raw == "" {
		return nil, err
	}
	return decodePolicyDocument([]byte(raw))
}

// Preserve arbitrary policy fields, exact JSON numbers and the previous
// canonical serialization. Dynamic JSON stays inside this codec.
func decodePolicyDocument(raw []byte) (PolicyDocument, error) {
	var document any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	if document == nil {
		return nil, nil
	}
	canonical, err := json.Marshal(document)
	return PolicyDocument(canonical), err
}

// An absent policy must remain a nil interface in the existing wire envelope:
// omit value, rather than introducing a typed nil encoded as "value": null.
func policySetting(document PolicyDocument) SettingValue {
	if document == nil {
		return nil
	}
	return document
}

func ReadQuota(ctx context.Context, s ReadQuotaClient, bucket string) (QuotaSetting, error) {
	q, err := s.GetBucketQuota(ctx, bucket)
	size := q.Size
	if size == 0 {
		size = q.Quota
	}
	return QuotaSetting{Size: size, Type: q.Type, Rate: q.Rate, Requests: q.Requests}, err
}

func ReadTags(ctx context.Context, s ReadTagsClient, bucket string) (TagSetting, error) {
	value, err := s.GetBucketTagging(ctx, bucket)
	if minio.ToErrorResponse(err).Code == "NoSuchTagSet" {
		err = nil
	}
	values := TagSetting{}
	if value != nil {
		values = TagSetting(value.ToMap())
	}
	return values, err
}
