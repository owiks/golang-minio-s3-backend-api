package config

import (
	"strings"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/operationpolicy"
)

func TestOperatorPolicyEnvironment(t *testing.T) {
	cfg, err := LoadEnv(func(string) string { return "" })
	if err != nil || cfg.Operations != (operationpolicy.Policy{}) {
		t.Fatal(cfg.Operations, err)
	}
	for _, key := range []string{"API_READ_ONLY", "API_DISABLE_UPLOADS", "API_DISABLE_DELETES", "API_DISABLE_ADMIN_CHANGES"} {
		t.Run(key, func(t *testing.T) {
			cfg, err := LoadEnv(func(name string) string {
				if name == key {
					return " true "
				}
				return ""
			})
			if err != nil {
				t.Fatal(err)
			}
			want := operationpolicy.Policy{}
			switch key {
			case "API_READ_ONLY":
				want.ReadOnly = true
			case "API_DISABLE_UPLOADS":
				want.DisableUploads = true
			case "API_DISABLE_DELETES":
				want.DisableDeletes = true
			case "API_DISABLE_ADMIN_CHANGES":
				want.DisableAdminChanges = true
			}
			if cfg.Operations != want {
				t.Fatal(cfg.Operations, want)
			}
			_, err = LoadEnv(func(name string) string {
				if name == key {
					return "private-invalid-value"
				}
				return ""
			})
			if err == nil || !strings.Contains(err.Error(), key) || strings.Contains(err.Error(), "private-invalid-value") {
				t.Fatal("unsafe or accepted invalid switch", err)
			}
		})
	}
}
