package revision

import "testing"

// Values recorded with the implementation before it left runtime. Existing
// confirmations must remain valid across this structural refactoring.
func TestRevisionCompatibility(t *testing.T) {
	first := `{"Statement":[{"Action":["s3:PutObject","s3:GetObject"],"Effect":"Allow","Resource":"*"}],"n":9007199254740993}`
	reordered := `{"n":9007199254740993,"Statement":[{"Resource":"*","Effect":"Allow","Action":["s3:GetObject","s3:PutObject"]}]}`
	for _, raw := range []string{first, reordered} {
		if got := Of([]byte(raw)); got != "f94458c7b1209d1fb952e56718431a30cae403d7f888be799888070e3159dea4" {
			t.Fatal("canonical revision changed", got)
		}
	}
	nearby := `{"n":9007199254740992,"Statement":[{"Resource":"*","Effect":"Allow","Action":["s3:GetObject","s3:PutObject"]}]}`
	if got := Of([]byte(nearby)); got != "21e6250bdb81c3a104f7f567a09422a51634e67e40979b832f5ff82489676758" {
		t.Fatal("large integer precision lost", got)
	}
	if got := Of([]byte("not json")); got != "7ccfa1fbf3940e6f0c0375d87c0f9235a50514e14cb427bdfaf5077987b26ccf" {
		t.Fatal("raw revision fallback changed", got)
	}
}
