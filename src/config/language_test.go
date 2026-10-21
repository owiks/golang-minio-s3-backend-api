package config

import (
	"strings"
	"testing"
)

func TestLanguageConfiguration(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{{"", "auto"}, {"auto", "auto"}, {" en ", "en"}, {"ru", "ru"}} {
		c, err := LoadEnv(env(map[string]string{"API_LANGUAGE": tc.raw, "LANG": "ru_RU.UTF-8", "LC_ALL": "ru_RU.UTF-8"}))
		if err != nil || c.Language != tc.want {
			t.Fatal(tc, c.Language, err)
		}
	}
	if Default().Language != "auto" || (Config{}).WithDefaults().Language != "auto" {
		t.Fatal("missing default")
	}
	for _, raw := range []string{"RU", "ru-RU", "de", "invalid-secret"} {
		_, err := LoadEnv(env(map[string]string{"API_LANGUAGE": raw}))
		if err == nil || err.Error() != "invalid API_LANGUAGE" || strings.Contains(err.Error(), raw) {
			t.Fatal(raw, err)
		}
	}
}
