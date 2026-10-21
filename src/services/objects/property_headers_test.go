package objects

import "testing"

func TestPropertyHeaderCatalogReturnsIndependentValues(t *testing.T) {
	copy := propertyHeaders()
	copy[0] = "Authorization"
	if propertyHeaders()[0] != "Content-Type" {
		t.Fatal("header catalog changed through a returned value")
	}
	for _, forbidden := range []string{"Authorization", "Host", "X-Amz-Acl", "Content-Length", "content-type"} {
		if err := ValidatePropertyPatch(map[string]string{forbidden: "value"}, nil, nil, "", ""); err == nil {
			t.Fatal("unsupported header accepted", forbidden)
		}
	}
	for _, allowed := range propertyHeaders() {
		value := "value"
		if allowed == "Content-Type" {
			value = "application/json"
		}
		if err := ValidatePropertyPatch(map[string]string{allowed: value}, nil, nil, "", ""); err != nil {
			t.Fatal("supported header rejected", allowed, err)
		}
	}
}
