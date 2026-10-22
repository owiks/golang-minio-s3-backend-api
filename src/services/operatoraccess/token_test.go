package operatoraccess

import (
	"strings"
	"testing"
)

func TestOperatorTokenIsIndependentAndStrict(t *testing.T) {
	secret := strings.Repeat("a", 64)
	for _, headers := range [][]string{{"Bearer " + secret}, {"bearer " + secret}} {
		if !Match(secret, headers) {
			t.Fatal("valid operator token rejected")
		}
	}
	for _, headers := range [][]string{nil, {""}, {"Basic " + secret}, {"Bearer " + secret, "Bearer " + secret}, {"Bearer  " + secret}, {"Bearer " + secret + " "}, {"Bearer " + strings.Repeat("b", 64)}, {"Bearer cookie-token"}, {"Bearer " + secret + ",Bearer " + secret}} {
		if Match(secret, headers) {
			t.Fatal("invalid operator authentication accepted")
		}
	}
	if Match("", []string{"Bearer " + secret}) || Match("short", []string{"Bearer short"}) {
		t.Fatal("disabled or invalid configured credential accepted")
	}
}

func TestTokenBoundsAndCharacters(t *testing.T) {
	for _, value := range []string{"", strings.Repeat("a", 31), strings.Repeat("a", 513), strings.Repeat("a", 32) + "\n", strings.Repeat("a", 32) + "é", strings.Repeat("a", 32) + ",x", strings.Repeat("a", 32) + "\t"} {
		if ValidToken(value) {
			t.Fatal("invalid secret accepted")
		}
	}
	if !ValidToken(strings.Repeat("a", 32)) || !ValidToken(strings.Repeat("b", 512)) {
		t.Fatal("valid length rejected")
	}
}
