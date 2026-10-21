package buckets

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/tags"

	"minio-web/api/src/services/apperror"
)

// Each fake implements only its own setting's read/write port. Passing these
// to a scenario would not compile with the former shared SDK dependency.
type settingCalls struct {
	t       *testing.T
	ctx     context.Context
	calls   []string
	reads   int
	failOn  string
	failure error
}

func (s *settingCalls) enter(ctx context.Context, bucket, operation string) error {
	s.t.Helper()
	if ctx != s.ctx || bucket != "test-bucket" {
		s.t.Fatal("setting scope/context changed")
	}
	if operation == "read" {
		s.reads++
		operation = map[int]string{1: "read:1", 2: "read:2"}[s.reads]
	}
	s.calls = append(s.calls, operation)
	if err := ctx.Err(); err != nil {
		return err
	}
	if operation == s.failOn {
		return s.failure
	}
	return nil
}

type policyPort struct {
	*settingCalls
	raw, written, normalized string
}

func (p *policyPort) GetBucketPolicy(ctx context.Context, bucket string) (string, error) {
	if err := p.enter(ctx, bucket, "read"); err != nil {
		return "", err
	}
	return p.raw, nil
}
func (p *policyPort) SetBucketPolicy(ctx context.Context, bucket, raw string) error {
	if err := p.enter(ctx, bucket, "write"); err != nil {
		return err
	}
	p.written, p.raw = raw, raw
	if p.normalized != "" {
		p.raw = p.normalized
	}
	return nil
}

type quotaPort struct {
	*settingCalls
	value, written madmin.BucketQuota
	readHook       func()
}

func (p *quotaPort) GetBucketQuota(ctx context.Context, bucket string) (madmin.BucketQuota, error) {
	if err := p.enter(ctx, bucket, "read"); err != nil {
		return madmin.BucketQuota{}, err
	}
	if p.readHook != nil {
		p.readHook()
		p.readHook = nil
	}
	return p.value, nil
}
func (p *quotaPort) SetBucketQuota(ctx context.Context, bucket string, value *madmin.BucketQuota) error {
	if err := p.enter(ctx, bucket, "write"); err != nil {
		return err
	}
	p.written, p.value = *value, *value
	return nil
}

type tagsPort struct {
	*settingCalls
	values         map[string]string
	sets, removals int
}

func (p *tagsPort) GetBucketTagging(ctx context.Context, bucket string) (*tags.Tags, error) {
	if err := p.enter(ctx, bucket, "read"); err != nil {
		return nil, err
	}
	if len(p.values) == 0 {
		return nil, minio.ErrorResponse{Code: "NoSuchTagSet"}
	}
	return tags.NewTags(p.values, false)
}
func (p *tagsPort) SetBucketTagging(ctx context.Context, bucket string, value *tags.Tags) error {
	if err := p.enter(ctx, bucket, "write"); err != nil {
		return err
	}
	p.sets++
	p.values = value.ToMap()
	return nil
}
func (p *tagsPort) RemoveBucketTagging(ctx context.Context, bucket string) error {
	if err := p.enter(ctx, bucket, "write"); err != nil {
		return err
	}
	p.removals++
	p.values = map[string]string{}
	return nil
}

func TestTypedSettingChangesPreserveFailureAndReadbackContract(t *testing.T) {
	for _, kind := range []string{"policy", "quota", "tags"} {
		for _, scenario := range []string{"success", "read-failure", "stale", "missing-revision", "write-failure", "readback-failure", "cancelled"} {
			t.Run(kind+"/"+scenario, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				failure := apperror.New(403, "AccessDenied", "denied")
				calls := &settingCalls{t: t, ctx: ctx, failure: failure}
				switch scenario {
				case "read-failure":
					calls.failOn = "read:1"
				case "write-failure":
					calls.failOn = "write"
				case "readback-failure":
					calls.failOn = "read:2"
				case "cancelled":
					cancel()
				}
				var expected string
				var change func(string) (DetailField, error)
				switch kind {
				case "policy":
					p := &policyPort{settingCalls: calls, raw: `{"Statement":[],"Version":"2012-10-17"}`}
					expected = SettingField(PolicyDocument(p.raw), nil).Revision
					change = func(rev string) (DetailField, error) {
						return PolicyChange(ctx, p, "test-bucket", UpdateBucketPolicyRequest{Document: json.RawMessage(`{"Version":"2012-10-17","Statement":[],"FutureNumber":9007199254740993}`), Revision: rev})
					}
				case "quota":
					p := &quotaPort{settingCalls: calls, value: madmin.BucketQuota{Size: 100, Type: madmin.HardQuota, Rate: 7, Requests: 9}}
					expected = SettingField(QuotaSetting{Size: 100, Type: madmin.HardQuota, Rate: 7, Requests: 9}, nil).Revision
					size := uint64(200)
					change = func(rev string) (DetailField, error) {
						return QuotaChange(ctx, p, "test-bucket", UpdateBucketQuotaRequest{Size: &size, Revision: rev})
					}
				case "tags":
					p := &tagsPort{settingCalls: calls, values: map[string]string{"owner": "old"}}
					expected = SettingField(TagSetting(p.values), nil).Revision
					change = func(rev string) (DetailField, error) {
						return TagsChange(ctx, p, "test-bucket", UpdateBucketTagsRequest{Tags: []TagInput{{Key: "owner", Value: "new"}}, Revision: rev})
					}
				}
				if scenario == "stale" {
					expected = "stale"
				}
				if scenario == "missing-revision" {
					expected = ""
				}
				value, err := change(expected)
				wantCalls := []string{"read:1", "write", "read:2"}
				switch scenario {
				case "success":
					if err != nil || value.Error != "" || value.Revision == "" || value.Value == nil {
						t.Fatal("successful write lost readback", value, err)
					}
				case "readback-failure":
					if err != nil || value.Value != nil || value.Revision != "" || value.ErrorCode != "AccessDenied" || value.Error != "denied" {
						t.Fatal("acknowledged write became retryable or stale value leaked", value, err)
					}
					raw, _ := json.Marshal(value)
					if string(raw) != `{"error":"denied","errorCode":"AccessDenied"}` {
						t.Fatal("field error wire format changed", string(raw))
					}
				case "write-failure":
					wantCalls = []string{"read:1", "write"}
					if !errors.Is(err, failure) || value != (DetailField{}) {
						t.Fatal("write failure hidden", value, err)
					}
				default:
					wantCalls = []string{"read:1"}
					if value != (DetailField{}) || err == nil {
						t.Fatal("failed preflight became success", value, err)
					}
					if scenario == "stale" || scenario == "missing-revision" {
						if apperror.Public(err).Code != "BucketSettingChanged" || apperror.Public(err).Status != 409 {
							t.Fatal("revision rejection changed", err)
						}
					} else if scenario == "cancelled" {
						if !errors.Is(err, context.Canceled) {
							t.Fatal("cancellation lost", err)
						}
					} else if !errors.Is(err, failure) {
						t.Fatal("read failure hidden", err)
					}
				}
				if !reflect.DeepEqual(calls.calls, wantCalls) {
					t.Fatal("unexpected mutation/readback or retry", calls.calls, wantCalls)
				}
			})
		}
	}
}

func TestPolicyChangePreservesOpaqueJSONAndServerRevision(t *testing.T) {
	ctx := context.Background()
	input := json.RawMessage(`{"Version":"2012-10-17","Statement":[{"Resource":"arn:aws:s3:::bucket/*","Principal":"*","Effect":"Allow","Action":"s3:GetObject"}],"FutureNumber":9007199254740993,"Future":{"Unicode":"тест","Small":1e-7}}`)
	want := `{"Future":{"Small":1e-7,"Unicode":"тест"},"FutureNumber":9007199254740993,"Statement":[{"Action":"s3:GetObject","Effect":"Allow","Principal":"*","Resource":"arn:aws:s3:::bucket/*"}],"Version":"2012-10-17"}`
	// MinIO may normalize scalar actions into arrays. Return its representation.
	server := strings.Replace(want, `"Action":"s3:GetObject"`, `"Action":["s3:GetObject"]`, 1)
	p := &policyPort{settingCalls: &settingCalls{t: t, ctx: ctx}, normalized: server}
	value, err := PolicyChange(ctx, p, "test-bucket", UpdateBucketPolicyRequest{Document: input, Revision: SettingField(nil, nil).Revision})
	if err != nil || p.written != want {
		t.Fatal("policy fields, number precision or serialization changed", p.written, err)
	}
	stored, ok := value.Value.(PolicyDocument)
	if !ok || string(stored) != server || value.Revision != SettingField(PolicyDocument(server), nil).Revision {
		t.Fatal("server representation/revision lost", value)
	}
	p.normalized = ""
	p.calls, p.reads = nil, 0
	value, err = PolicyChange(ctx, p, "test-bucket", UpdateBucketPolicyRequest{Document: json.RawMessage(" \nnull "), Revision: value.Revision})
	raw, _ := json.Marshal(value)
	if err != nil || p.written != "" || value.Value != nil || value.Revision != SettingField(nil, nil).Revision || strings.Contains(string(raw), `"value"`) {
		t.Fatal("policy deletion changed absent-value envelope", string(raw), err)
	}
}

func TestQuotaChangePreservesOtherLimitsAndLegacyCapacity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		before madmin.BucketQuota
		size   uint64
		kind   madmin.QuotaType
	}{
		{"legacy", madmin.BucketQuota{Quota: 100, Rate: 7, Requests: 9, Type: madmin.HardQuota}, 0, madmin.HardQuota},
		{"clear", madmin.BucketQuota{Size: 100, Type: madmin.HardQuota}, 0, ""},
		{"set", madmin.BucketQuota{Size: 5, Quota: 100, Rate: 7, Requests: 9}, 9007199254740991, madmin.HardQuota},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			p := &quotaPort{settingCalls: &settingCalls{t: t, ctx: ctx}, value: tc.before}
			current, err := ReadQuota(ctx, p, "test-bucket")
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "legacy" && current.Size != 100 || tc.name == "set" && current.Size != 5 {
				t.Fatal("legacy size fallback changed", current)
			}
			p.calls, p.reads = nil, 0
			value, err := QuotaChange(ctx, p, "test-bucket", UpdateBucketQuotaRequest{Size: &tc.size, Revision: SettingField(current, nil).Revision})
			if err != nil || p.written.Size != tc.size || p.written.Rate != tc.before.Rate || p.written.Requests != tc.before.Requests || p.written.Type != tc.kind {
				t.Fatal("independent quota limits lost", p.written, err)
			}
			stored, ok := value.Value.(QuotaSetting)
			if !ok || stored.Size != tc.size || stored.Rate != tc.before.Rate || stored.Requests != tc.before.Requests {
				t.Fatal("quota readback changed", value)
			}
		})
	}
}

func TestTagsChangeCreatesAndRemovesThroughOwnPort(t *testing.T) {
	ctx := context.Background()
	p := &tagsPort{settingCalls: &settingCalls{t: t, ctx: ctx}}
	initial, err := ReadTags(ctx, p, "test-bucket")
	if err != nil || initial == nil || len(initial) != 0 {
		t.Fatal("NoSuchTagSet not normalized", initial, err)
	}
	p.calls, p.reads = nil, 0
	value, err := TagsChange(ctx, p, "test-bucket", UpdateBucketTagsRequest{Tags: []TagInput{{Key: "owner", Value: "team-one"}, {Key: "empty", Value: ""}}, Revision: SettingField(initial, nil).Revision})
	if err != nil || p.sets != 1 || p.removals != 0 || !reflect.DeepEqual(p.values, map[string]string{"owner": "team-one", "empty": ""}) {
		t.Fatal("tags not preserved", value, err)
	}
	p.calls, p.reads = nil, 0
	value, err = TagsChange(ctx, p, "test-bucket", UpdateBucketTagsRequest{Tags: []TagInput{}, Revision: value.Revision})
	raw, _ := json.Marshal(value)
	if err != nil || p.sets != 1 || p.removals != 1 || !strings.Contains(string(raw), `"value":{}`) || value.Revision != SettingField(TagSetting{}, nil).Revision {
		t.Fatal("empty tags lost or deletion changed", string(raw), err)
	}
}

func TestPolicyReadPreservesAbsentAndMalformedStates(t *testing.T) {
	for _, raw := range []string{"", "null", " \nnull "} {
		ctx := context.Background()
		p := &policyPort{settingCalls: &settingCalls{t: t, ctx: ctx}, raw: raw}
		value, err := ReadPolicy(ctx, p, "test-bucket")
		if err != nil || value != nil || policySetting(value) != nil {
			t.Fatal("absent policy gained a typed-nil wire value", raw, value, err)
		}
	}
	ctx := context.Background()
	p := &policyPort{settingCalls: &settingCalls{t: t, ctx: ctx}, raw: "{"}
	_, err := PolicyChange(ctx, p, "test-bucket", UpdateBucketPolicyRequest{Document: json.RawMessage(`{"Version":"2012-10-17","Statement":[]}`), Revision: SettingField(nil, nil).Revision})
	if err == nil || !reflect.DeepEqual(p.calls, []string{"read:1"}) {
		t.Fatal("malformed stored policy treated as absent and overwritten", p.calls, err)
	}
}

func TestQuotaChangeSnapshotsValidatedSizeBeforeRead(t *testing.T) {
	ctx := context.Background()
	size := uint64(200)
	p := &quotaPort{settingCalls: &settingCalls{t: t, ctx: ctx}, value: madmin.BucketQuota{Size: 100}, readHook: func() { size = 9007199254740992 }}
	_, err := QuotaChange(ctx, p, "test-bucket", UpdateBucketQuotaRequest{Size: &size, Revision: SettingField(QuotaSetting{Size: 100}, nil).Revision})
	if err != nil || p.written.Size != 200 {
		t.Fatal("validated input changed during the upstream read", p.written.Size, err)
	}
}
