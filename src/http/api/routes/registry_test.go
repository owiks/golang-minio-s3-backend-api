package routes

import (
	"net/http"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

func registryRoute(path, suffix, method string) APIRoute {
	return APIRoute{Path: path, Tag: "fixture", Operations: []Operation{{Handler: func(*runtime.State, http.ResponseWriter, *http.Request, *runtime.Session, string) error { return nil }, Suffix: suffix, Method: method, Summary: "fixture", Input: Inputs(), Controller: "Fixture", Response: struct{}{}}}}
}

func TestRegistryRequiresAndSelectsEachOperationHandler(t *testing.T) {
	route := registryRoute("items", "", "GET")
	route.Operations[0].Handler = nil
	if _, err := NewRegistry([]APIRoute{route}); err == nil {
		t.Fatal("operation without handler accepted")
	}
	called := ""
	route = registryRoute("items", "", "GET")
	route.Operations[0].Handler = func(*runtime.State, http.ResponseWriter, *http.Request, *runtime.Session, string) error {
		called = "read"
		return nil
	}
	update := route.Operations[0]
	update.Method = "PUT"
	update.Handler = func(*runtime.State, http.ResponseWriter, *http.Request, *runtime.Session, string) error {
		called = "write"
		return nil
	}
	route.Operations = append(route.Operations, update)
	registry, err := NewRegistry([]APIRoute{route})
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "PUT"} {
		match := registry.Match(method, "/api/sessions/session/items")
		if !match.MethodAllowed {
			t.Fatal("operation not found")
		}
		if err := match.Operation.Handler(nil, nil, nil, nil, match.Rest); err != nil {
			t.Fatal(err)
		}
		want := "read"
		if method == "PUT" {
			want = "write"
		}
		if called != want {
			t.Fatal("wrong operation handler", method, called)
		}
	}
}
func TestRegistryRejectsAmbiguity(t *testing.T) {
	for _, tc := range []struct {
		name    string
		catalog []APIRoute
	}{
		{"duplicate", []APIRoute{registryRoute("items", "{name}", "GET"), registryRoute("items", "{name}", "GET")}},
		{"renamed parameter", []APIRoute{registryRoute("items", "{name}", "GET"), registryRoute("items", "{id}", "POST")}},
		{"crossing patterns", []APIRoute{registryRoute("items", "{name}/details", "GET"), registryRoute("items", "fixed/{view}", "GET")}},
		{"empty segment", []APIRoute{registryRoute("items/", "{name}", "GET")}},
		{"malformed parameter", []APIRoute{registryRoute("items", "{bad", "GET")}},
		{"duplicate parameter", []APIRoute{registryRoute("items", "{sessionID}", "GET")}},
		{"invalid method", []APIRoute{registryRoute("items", "", "CONNECT")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewRegistry(tc.catalog); err == nil {
				t.Fatal("invalid registry accepted")
			}
		})
	}
}
func TestRegistrySelectsLiteralBeforeMethod(t *testing.T) {
	r, err := NewRegistry([]APIRoute{registryRoute("items", "{name}", "GET"), registryRoute("items", "fixed", "POST")})
	if err != nil {
		t.Fatal(err)
	}
	m := r.Match("GET", "/api/sessions/secret/items/fixed")
	if !m.Found || m.MethodAllowed || len(m.Allow) != 1 || m.Allow[0] != "POST" {
		t.Fatalf("wrong-method literal fell through to dynamic route: %+v", m)
	}
	m = r.Match("GET", "/api/sessions/secret/items/report")
	if !m.MethodAllowed || m.Params["name"] != "report" || m.Rest != "report" {
		t.Fatalf("dynamic resolution: %+v", m)
	}
	for _, path := range []string{"/api/sessions/secret/items/report/extra", "/api/sessions/secret/items/", "/api/sessions//items/report"} {
		if r.Match("GET", path).Found {
			t.Fatal("partial or empty parameter matched", path)
		}
	}
}
func TestRegistryOwnsCatalogMetadata(t *testing.T) {
	input := []APIRoute{registryRoute("items", "{name}", "PUT")}
	input[0].Operations[0].Request = struct {
		Revision string `json:"revision"`
	}{}
	input[0].Operations[0].Required = []string{"revision"}
	r, err := NewRegistry(input)
	if err != nil {
		t.Fatal(err)
	}
	input[0].Operations[0].Long = true
	input[0].Operations[0].Required[0] = "changed"
	snapshot := r.Catalog()
	snapshot[0].Operations[0].Method = "DELETE"
	snapshot[0].Operations[0].Required[0] = "changed"
	m := r.Match("PUT", "/api/sessions/secret/items/report")
	if !m.MethodAllowed || m.Operation.Long || m.Operation.Required[0] != "revision" || m.Route.Operations != nil {
		t.Fatal("caller changed the live registry")
	}
}

func TestRegistryRestUsesSelectedMethod(t *testing.T) {
	r, err := NewRegistry([]APIRoute{registryRoute("items", "{name}", "GET"), registryRoute("items/{name}", "", "PUT")})
	if err != nil {
		t.Fatal(err)
	}
	if m := r.Match("PUT", "/api/sessions/private/items/name"); !m.MethodAllowed || m.Rest != "" {
		t.Fatal("suffix came from another method")
	}
	if m := r.Match("GET", "/api/sessions/private/items/name"); !m.MethodAllowed || m.Rest != "name" {
		t.Fatal("dynamic suffix lost")
	}
}

func TestRegistryRequiresUnambiguousResponseModels(t *testing.T) {
	for _, op := range []Operation{
		{},
		{Response: struct{}{}, Responses: []Response{{Status: 200}}},
		{Responses: []Response{{Status: 200, MediaType: "application/json"}}},
		{Responses: []Response{{Status: 200, MediaType: "application/json", Model: struct{}{}, Binary: true}}},
		{Responses: []Response{{Status: 500}}},
		{Responses: []Response{{Status: 200}, {Status: 200}}},
	} {
		route := registryRoute("items", "", "GET")
		op.Input = Inputs()
		op.Method = "GET"
		op.Controller = "Fixture"
		op.Summary = "fixture"
		route.Operations = []Operation{op}
		if _, err := NewRegistry([]APIRoute{route}); err == nil {
			t.Fatal("missing or ambiguous response accepted")
		}
	}
	route := registryRoute("items", "", "GET")
	route.Operations[0].Response = nil
	route.Operations[0].Responses = []Response{{Status: 200, MediaType: "application/json", Variants: []any{struct{}{}, ""}}}
	registry, err := NewRegistry([]APIRoute{route})
	if err != nil {
		t.Fatal(err)
	}
	route.Operations[0].Responses[0].Status = 204
	snapshot := registry.Catalog()
	snapshot[0].Operations[0].Responses[0].Variants[0] = nil
	match := registry.Match("GET", "/api/sessions/private/items")
	match.Operation.Responses[0].Variants[0] = nil
	next := registry.Match("GET", "/api/sessions/private/items")
	if next.Operation.Responses[0].Status != 200 || next.Operation.Responses[0].Variants[0] == nil {
		t.Fatal("response metadata is shared with caller")
	}
}

func TestRegistryRejectsInvalidInputContracts(t *testing.T) {
	for _, input := range []Operation{
		{Input: nil},
		{Input: Inputs(), Request: struct{}{}, BinaryRequest: true},
		{Input: Inputs(), Required: []string{"missing"}},
		{Input: Inputs(Query("q", false, 0), Query("q", true, 0))},
		{Input: Inputs(Header("Origin", true), Header("origin", true))},
		{Input: Inputs(IntegerQuery("limit", true, 10, 1))},
		{Input: Inputs(Parameter{Name: "q", In: "cookie", Type: "string"})},
		{Input: Inputs(Parameter{Name: "q", In: "query", Type: "object"})},
	} {
		route := registryRoute("items", "", "POST")
		op := route.Operations[0]
		op.Input, op.Request, op.BinaryRequest, op.Required = input.Input, input.Request, input.BinaryRequest, input.Required
		route.Operations[0] = op
		if _, err := NewRegistry([]APIRoute{route}); err == nil {
			t.Fatal("invalid input contract accepted", input)
		}
	}
	for _, method := range []string{"GET", "HEAD"} {
		route := registryRoute("items", "", method)
		route.Operations[0].Request = struct{}{}
		if _, err := NewRegistry([]APIRoute{route}); err == nil {
			t.Fatal("read operation has a request body", method)
		}
	}
}

func TestRegistryOwnsInputMetadata(t *testing.T) {
	route := registryRoute("items", "", "GET")
	route.Operations[0].Input = Inputs(EnumQuery("kind", true, "logs", "trace"), IntegerQuery("seconds", true, 1, 10)).Describe("fixture")
	registry, err := NewRegistry([]APIRoute{route})
	if err != nil {
		t.Fatal(err)
	}
	route.Operations[0].Input.Parameters[0].Enum[0] = "changed"
	*route.Operations[0].Input.Parameters[1].Maximum = 999
	snapshot := registry.Catalog()
	snapshot[0].Operations[0].Input.Description = "changed"
	snapshot[0].Operations[0].Input.Parameters[0].Enum[0] = "changed"
	match := registry.Match("GET", "/api/sessions/private/items")
	*match.Operation.Input.Parameters[1].Minimum = 999
	next := registry.Match("GET", "/api/sessions/private/items")
	if next.Operation.Input.Description != "fixture" || next.Operation.Input.Parameters[0].Enum[0] != "logs" || *next.Operation.Input.Parameters[1].Minimum != 1 || *next.Operation.Input.Parameters[1].Maximum != 10 {
		t.Fatal("input metadata aliases live registry")
	}
}

func TestRegistryPreservesTotalLiteralPriority(t *testing.T) {
	r, err := NewRegistry([]APIRoute{
		registryRoute("items", "{name}/specific/detail", "POST"),
		registryRoute("items", "fixed/{view}/{id}", "GET"),
	})
	if err != nil {
		t.Fatal(err)
	}
	m := r.Match("GET", "/api/sessions/private/items/fixed/specific/detail")
	if !m.Found || m.MethodAllowed || m.Template != "/api/sessions/{sessionID}/items/{name}/specific/detail" || len(m.Allow) != 1 || m.Allow[0] != "POST" {
		t.Fatalf("segment precedence replaced catalog priority: %+v", m)
	}
}

func TestRegistryDoesNotInterpretParametersOrLiteralAsterisks(t *testing.T) {
	for _, tc := range []struct {
		suffix, value, key string
	}{
		{"{name:[0-9]+}", "letters", "name:[0-9]+"},
		{"{*}", "value", "*"},
		{"a*b", "a*b", ""},
		{"a*b$", "a*b$", ""},
		{`a*b\$`, `a*b\$`, ""},
		{`a*b\`, `a*b\`, ""},
		{"^a*b$", "^a*b$", ""},
	} {
		r, err := NewRegistry([]APIRoute{registryRoute("items", tc.suffix, "GET")})
		if err != nil {
			t.Fatal(err)
		}
		m := r.Match("GET", "/api/sessions/private/items/"+tc.value)
		if !m.MethodAllowed || tc.key != "" && m.Params[tc.key] != tc.value {
			t.Fatalf("catalog acquired chi pattern semantics: %+v", m)
		}
		if tc.key == "" {
			for _, value := range []string{"another", tc.value + "extra", "extra" + tc.value, tc.value + "/extra", tc.value + "\n"} {
				if r.Match("GET", "/api/sessions/private/items/"+value).Found {
					t.Errorf("literal %q matched %q", tc.value, value)
				}
			}
		}
	}
}
