package keys_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/minio/madmin-go/v3"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/keys"
)

// Deliberately has no STS, list, create, delete, session or credential methods.
type changeClient struct {
	actor    string
	info     madmin.InfoServiceAccountResp
	readErr  error
	writeErr error
	calls    []string
	contexts []context.Context
	written  madmin.UpdateServiceAccountReq
}

var _ keys.ChangeKeyClient = (*changeClient)(nil)

func (c *changeClient) ActorName() string { return c.actor }
func (c *changeClient) InfoServiceAccount(ctx context.Context, name string) (madmin.InfoServiceAccountResp, error) {
	c.calls = append(c.calls, "read:"+name)
	c.contexts = append(c.contexts, ctx)
	return c.info, c.readErr
}
func (c *changeClient) UpdateServiceAccount(ctx context.Context, name string, opts madmin.UpdateServiceAccountReq) error {
	c.calls = append(c.calls, "write:"+name)
	c.contexts = append(c.contexts, ctx)
	c.written = opts
	return c.writeErr
}

type detailClient struct{ *changeClient }

func (c detailClient) TemporaryAccountInfo(context.Context, string) (madmin.TemporaryAccountInfoResp, error) {
	panic("a service-key command must not read STS")
}

func newChangeClient() *changeClient {
	return &changeClient{actor: "actor", info: madmin.InfoServiceAccountResp{
		ParentUser: "owner", Name: "old-name", Description: "old description",
		AccountStatus: "on", ImpliedPolicy: true,
	}}
}

func keyRevision(t *testing.T, c *changeClient) string {
	t.Helper()
	d, err := keys.KeyDetail(context.Background(), detailClient{c}, "target", false)
	if err != nil || d.Revision == "" {
		t.Fatalf("cannot prepare revision: %v", err)
	}
	c.calls, c.contexts = nil, nil
	return d.Revision
}

type command func(context.Context, *changeClient, string) (keys.GeneratedKey, error)

// Each invocation uses exactly one request type and its own entry point.
var commands = map[string]command{
	"edit": func(ctx context.Context, c *changeClient, revision string) (keys.GeneratedKey, error) {
		return keys.GeneratedKey{}, keys.EditKey(ctx, c, "target", keys.KeyInput{
			Name: "new-name", Description: "new description", Policy: json.RawMessage("null"), Revision: revision,
		})
	},
	"status": func(ctx context.Context, c *changeClient, revision string) (keys.GeneratedKey, error) {
		return keys.GeneratedKey{}, keys.SetKeyStatus(ctx, c, "target", keys.StatusRequest{Enabled: new(false), Revision: revision})
	},
	"rotate": func(ctx context.Context, c *changeClient, revision string) (keys.GeneratedKey, error) {
		return keys.RotateKeySecret(ctx, c, "target", keys.RotateRequest{Confirmation: "target", Revision: revision})
	},
}

func assertCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil || apperror.Public(err).Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func TestKeyCommandsProtectCurrentKeyBeforeValidation(t *testing.T) {
	for name, run := range commands {
		t.Run(name, func(t *testing.T) {
			c := newChangeClient()
			c.actor = "target"
			pair, err := run(context.Background(), c, "")
			assertCode(t, err, "CurrentAccessKey")
			if len(c.calls) != 0 || pair != (keys.GeneratedKey{}) {
				t.Fatal("current key reached upstream or exposed a secret")
			}
		})
	}
}

func TestKeyCommandValidationPrecedesReads(t *testing.T) {
	for name, tc := range map[string]struct {
		run  func(*changeClient) error
		code string
	}{
		"missing-status": {func(c *changeClient) error {
			return keys.SetKeyStatus(context.Background(), c, "target", keys.StatusRequest{})
		}, "InvalidStatus"},
		"wrong-confirmation": {func(c *changeClient) error {
			_, err := keys.RotateKeySecret(context.Background(), c, "target", keys.RotateRequest{Confirmation: "wrong"})
			return err
		}, "ConfirmationRequired"},
		"name": {func(c *changeClient) error {
			return keys.EditKey(context.Background(), c, "target", keys.KeyInput{Name: "bad name", Policy: json.RawMessage("null")})
		}, "InvalidKeyName"},
		"description": {func(c *changeClient) error {
			return keys.EditKey(context.Background(), c, "target", keys.KeyInput{Description: strings.Repeat("x", 257), Policy: json.RawMessage("null")})
		}, "InvalidKeyDescription"},
		"policy": {func(c *changeClient) error {
			return keys.EditKey(context.Background(), c, "target", keys.KeyInput{Policy: json.RawMessage(`{}`)})
		}, "InvalidPolicy"},
		"owner": {func(c *changeClient) error {
			return keys.EditKey(context.Background(), c, "target", keys.KeyInput{Parent: "another-owner", Policy: json.RawMessage("null")})
		}, "ImmutableParent"},
	} {
		t.Run(name, func(t *testing.T) {
			c := newChangeClient()
			assertCode(t, tc.run(c), tc.code)
			if len(c.calls) != 0 {
				t.Fatal("invalid command reached upstream", c.calls)
			}
		})
	}
}

func TestKeyCommandsRevisionAndFailureBoundaries(t *testing.T) {
	for name, run := range commands {
		t.Run(name, func(t *testing.T) {
			for _, expected := range []string{"", "stale"} {
				t.Run("revision-"+expected, func(t *testing.T) {
					c := newChangeClient()
					pair, err := run(context.Background(), c, expected)
					assertCode(t, err, "AccessKeyChanged")
					if !reflect.DeepEqual(c.calls, []string{"read:target"}) || pair != (keys.GeneratedKey{}) {
						t.Fatal("revision conflict wrote or exposed a secret", c.calls)
					}
				})
			}
			for _, failure := range []error{errors.New("upstream failure"), madmin.ErrorResponse{Code: "AccessDenied"}, context.Canceled} {
				for _, phase := range []string{"read", "write"} {
					t.Run(phase+"-"+failure.Error(), func(t *testing.T) {
						c := newChangeClient()
						revision := keyRevision(t, c)
						if phase == "read" {
							c.readErr = failure
						} else {
							c.writeErr = failure
						}
						ctx, cancel := context.WithCancel(context.Background())
						defer cancel()
						if errors.Is(failure, context.Canceled) {
							cancel()
						}
						pair, err := run(ctx, c, revision)
						if !errors.Is(err, failure) || pair != (keys.GeneratedKey{}) {
							t.Fatalf("failure lost or secret exposed: %v", err)
						}
						want := []string{"read:target"}
						if phase == "write" {
							want = append(want, "write:target")
						}
						if !reflect.DeepEqual(c.calls, want) {
							t.Fatal(c.calls)
						}
						for _, received := range c.contexts {
							if received != ctx {
								t.Fatal("request context replaced")
							}
						}
					})
				}
			}
			t.Run("missing-key", func(t *testing.T) {
				c := newChangeClient()
				c.readErr = madmin.ErrorResponse{Code: "XMinioInvalidIAMCredentials"}
				_, err := run(context.Background(), c, "revision")
				assertCode(t, err, "AccessKeyNotFound")
				if !reflect.DeepEqual(c.calls, []string{"read:target"}) {
					t.Fatal(c.calls)
				}
			})
			for _, policy := range []string{"", "null", "{broken"} {
				t.Run("unreadable-policy-"+policy, func(t *testing.T) {
					c := newChangeClient()
					c.info.ImpliedPolicy, c.info.Policy = false, policy
					pair, err := run(context.Background(), c, "revision")
					assertCode(t, err, "UnreadableAccessKeyPolicy")
					if !reflect.DeepEqual(c.calls, []string{"read:target"}) || pair != (keys.GeneratedKey{}) {
						t.Fatal(c.calls)
					}
				})
			}
		})
	}
}

const exactPolicy = `{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Action":"s3:PutObject","Resource":"*"}],"Extension":{"large":9007199254740993,"fraction":1.2300}}`

func TestStatusAndRotationPreserveRestrictions(t *testing.T) {
	for name, run := range map[string]command{
		"disable": commands["status"],
		"enable": func(ctx context.Context, c *changeClient, rev string) (keys.GeneratedKey, error) {
			return keys.GeneratedKey{}, keys.SetKeyStatus(ctx, c, "target", keys.StatusRequest{Enabled: new(true), Revision: rev})
		},
		"rotate": commands["rotate"],
	} {
		for _, inherited := range []bool{true, false} {
			t.Run(name+"-inherited-"+map[bool]string{true: "yes", false: "no"}[inherited], func(t *testing.T) {
				c := newChangeClient()
				c.info.ImpliedPolicy, c.info.Policy = inherited, exactPolicy
				c.info.Expiration = new(time.Now().Add(time.Hour))
				pair, err := run(context.Background(), c, keyRevision(t, c))
				if err != nil {
					t.Fatal(err)
				}
				want := madmin.UpdateServiceAccountReq{}
				if !inherited {
					want.NewPolicy = json.RawMessage(exactPolicy)
				}
				switch name {
				case "enable":
					want.NewStatus = "on"
				case "disable":
					want.NewStatus = "off"
				case "rotate":
					if pair.AccessKey != "target" || len(pair.SecretKey) < 32 || pair.SecretKey != c.written.NewSecretKey {
						t.Fatal("incorrect rotation result")
					}
					want.NewSecretKey = pair.SecretKey
				}
				if !reflect.DeepEqual(c.written, want) || !reflect.DeepEqual(c.calls, []string{"read:target", "write:target"}) {
					t.Fatal("unrelated fields changed, policy rewritten, or write repeated")
				}
			})
		}
	}
}

func TestEditKeyExpirationAndMetadata(t *testing.T) {
	near := time.Now().UTC().Add(5 * time.Minute)
	future := time.Now().UTC().Add(2 * time.Hour)
	old := future.Add(-time.Hour)
	epoch := time.Unix(0, 0).UTC()
	for _, tc := range []struct {
		name                 string
		current, input, want *time.Time
		code                 string
	}{
		{"unlimited-unchanged", nil, nil, nil, ""},
		{"epoch-unchanged", &epoch, nil, nil, ""},
		{"near-expiry-unchanged", &near, &near, nil, ""},
		{"same-instant-other-zone", &future, new(future.In(time.FixedZone("offset", 3600))), nil, ""},
		{"changed", &old, &future, &future, ""},
		{"add-deadline", nil, &future, &future, ""},
		{"clear-deadline", &old, nil, &epoch, ""},
		{"changed-too-near", &old, &near, nil, "InvalidExpiration"},
		{"changed-too-far", nil, new(time.Now().Add(366 * 24 * time.Hour)), nil, "InvalidExpiration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newChangeClient()
			c.info.Expiration = tc.current
			input := keys.KeyInput{Name: "new-name", Description: "new description", Policy: json.RawMessage(exactPolicy), Expiration: tc.input, Revision: keyRevision(t, c)}
			err := keys.EditKey(context.Background(), c, "target", input)
			if tc.code != "" {
				assertCode(t, err, tc.code)
				if !reflect.DeepEqual(c.calls, []string{"read:target"}) {
					t.Fatal(c.calls)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := madmin.UpdateServiceAccountReq{NewName: input.Name, NewDescription: input.Description, NewPolicy: input.Policy, NewExpiration: tc.want}
			if !reflect.DeepEqual(c.written, want) || !reflect.DeepEqual(c.calls, []string{"read:target", "write:target"}) {
				t.Fatal("edit changed unrelated fields, deadline, or exact policy")
			}
		})
	}
	for _, field := range []string{"name", "description"} {
		t.Run("cannot-clear-"+field, func(t *testing.T) {
			c := newChangeClient()
			input := keys.KeyInput{Name: c.info.Name, Description: c.info.Description, Policy: json.RawMessage("null"), Revision: keyRevision(t, c)}
			if field == "name" {
				input.Name = ""
			} else {
				input.Description = ""
			}
			assertCode(t, keys.EditKey(context.Background(), c, "target", input), "EmptyKeyMetadata")
			if !reflect.DeepEqual(c.calls, []string{"read:target"}) {
				t.Fatal(c.calls)
			}
		})
	}
	t.Run("restore-inheritance", func(t *testing.T) {
		c := newChangeClient()
		c.info.ImpliedPolicy, c.info.Policy = false, exactPolicy
		_, err := commands["edit"](context.Background(), c, keyRevision(t, c))
		if err != nil || string(c.written.NewPolicy) != "{}" {
			t.Fatal("inheritance reset lost", err)
		}
	})
}
