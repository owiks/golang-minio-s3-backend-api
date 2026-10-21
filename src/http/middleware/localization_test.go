package middleware

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/requestctx"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/localization"
)

func TestChooseLanguage(t *testing.T) {
	for _, tc := range []struct{ mode, header, want string }{
		{"auto", "", "en"}, {"auto", "ru-RU, en;q=0.8", "ru"}, {"auto", "en;q=0.9, ru;q=0.5", "en"},
		{"auto", "de, ru;q=0.7", "ru"}, {"auto", "de, uk", "en"}, {"auto", "ru;q=0, en;q=0.1", "en"},
		{"auto", "ru;q=bad", "en"}, {"auto", "ru_@", "en"}, {"auto", strings.Repeat("ru,", 600), "en"},
		{"auto", "*", "en"}, {"en", "ru", "en"}, {"ru", "en", "ru"}, {"auto", "en-GB;q=0.3,ru-RU;q=0.8", "ru"},
		{"auto", "ru;q=2", "en"}, {"auto", "ru;q=NaN", "en"}, {"auto", "ru;q=Inf", "en"},
		{"auto", "en;q=-1,ru", "en"}, {"auto", "ru;q=1e-1", "en"}, {"auto", "ru;q=.9", "en"},
		{"auto", "ru;q=0.1234", "en"}, {"auto", "ru;q=1.001", "en"}, {"auto", "ru;Q=0.9", "ru"},
		{"auto", "ru_RU", "en"}, {"auto", "ru-", "en"}, {"auto", "ru-123456789", "en"},
		{"auto", "und-RU", "en"}, {"auto", "und-Cyrl", "en"}, {"auto", "und", "en"},
		{"auto", "und-RU;q=1,en;q=0.5", "en"}, {"auto", "und-Cyrl;q=1,ru;q=0.5", "ru"},
	} {
		if got := ChooseLanguage(tc.mode, tc.header); got != tc.want {
			t.Fatalf("%s %q: %s", tc.mode, tc.header, got)
		}
	}
}
func TestRequestLocalizerDoesNotChangeParent(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Add("Accept-Language", "en;q=0.2")
	r.Header.Add("Accept-Language", "ru-RU;q=0.9")
	c := localization.Default()
	localized := LocalizeRequest(r, "auto", c)
	if requestctx.Language(localized.Context()) != "ru" || !requestctx.AutomaticLanguage(localized.Context()) || requestctx.Language(r.Context()) != "en" {
		t.Fatal("request context leaked")
	}
	forced := LocalizeRequest(r, "en", c)
	if requestctx.Language(forced.Context()) != "en" || requestctx.AutomaticLanguage(forced.Context()) {
		t.Fatal("forced language lost")
	}
	r.Header.Add("Accept-Language", strings.Repeat("x", 1025))
	if requestctx.Language(LocalizeRequest(r, "auto", c).Context()) != "en" {
		t.Fatal("unbounded multivalue header")
	}
}
