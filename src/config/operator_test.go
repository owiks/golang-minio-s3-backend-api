package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOperatorCredentialDefaultsDisabledAndNeverSerializes(t *testing.T) {
	got, err := LoadEnv(env(nil))
	if err != nil || got.OperatorToken != "" {
		t.Fatal("operator access must be disabled by default", err)
	}
	secret := strings.Repeat("private-operator-", 4)
	got, err = LoadEnv(env(map[string]string{"API_OPERATOR_TOKEN": secret}))
	if err != nil || got.OperatorToken != secret {
		t.Fatal("operator credential rejected", err)
	}
	raw, err := json.Marshal(got)
	if err != nil || strings.Contains(string(raw), secret) || strings.Contains(string(raw), "OperatorToken") {
		t.Fatal("operator credential serialized")
	}
	for _, value := range []string{"short-private-value", secret + " ", " " + secret, secret + "\n", strings.Repeat("a", 513), " \t\r\n "} {
		_, err := LoadEnv(env(map[string]string{"API_OPERATOR_TOKEN": value}))
		if err == nil || err.Error() != "invalid API_OPERATOR_TOKEN" {
			t.Fatal("unsafe operator credential validation error", err)
		}
	}
}
