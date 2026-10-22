package routes

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
)

type entry struct {
	route     APIRoute
	operation Operation
	template  string
	parts     []string
	literals  int
}

// Registry is compiled once at startup. It owns its catalog so callers cannot
// change live dispatch, budgets or documentation by mutating a returned slice.
type Registry struct {
	entries []entry
	catalog []APIRoute
}

func cloneOperation(op Operation) Operation {
	op.Scopes = append([]string(nil), op.Scopes...)
	op.Input = cloneInput(op.Input)
	op.Required = append([]string(nil), op.Required...)
	op.Responses = append([]Response(nil), op.Responses...)
	for i := range op.Responses {
		op.Responses[i].Variants = append([]any(nil), op.Responses[i].Variants...)
	}
	return op
}
func cloneCatalog(catalog []APIRoute) []APIRoute {
	copy := append([]APIRoute(nil), catalog...)
	for i := range copy {
		copy[i].Operations = append([]Operation(nil), copy[i].Operations...)
		for j := range copy[i].Operations {
			copy[i].Operations[j] = cloneOperation(copy[i].Operations[j])
		}
	}
	return copy
}
func (r *Registry) Catalog() []APIRoute { return cloneCatalog(r.catalog) }

func NewRegistry(catalog []APIRoute) (*Registry, error) {
	r := &Registry{catalog: cloneCatalog(catalog)}
	for _, route := range r.catalog {
		if route.Tag == "" || len(route.Operations) == 0 {
			return nil, fmt.Errorf("incomplete API route: %s", route.Path)
		}
		for _, op := range route.Operations {
			if op.Handler == nil {
				return nil, fmt.Errorf("missing operation handler: %s %s", op.Method, route.Path)
			}
			if err := validateInput(op); err != nil {
				return nil, fmt.Errorf("%s: %w", route.Path, err)
			}
			if err := validateResponses(op); err != nil {
				return nil, fmt.Errorf("%s: %w", route.Path, err)
			}
			switch op.Method {
			case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS":
			default:
				return nil, fmt.Errorf("invalid API method: %s", op.Method)
			}
			if op.Controller == "" || op.Summary == "" || op.Mutation > AfterDecode {
				return nil, fmt.Errorf("incomplete API operation: %s", route.Path)
			}
			if op.InvalidatesAccess && (route.Public || op.Method != "POST" && op.Method != "PUT" && op.Method != "PATCH" && op.Method != "DELETE") {
				return nil, fmt.Errorf("invalid access invalidation: %s", route.Path)
			}
			template := route.Template(op)
			if route.Public && strings.HasPrefix(template, "/api/sessions/") {
				return nil, fmt.Errorf("public operation inside session namespace: %s", template)
			}
			if op.Mutation == AfterDecode && (route.Public || op.Method != "POST" || op.Long) {
				return nil, fmt.Errorf("invalid decoded mutation lock: %s", template)
			}
			if !strings.HasPrefix(template, "/") || strings.HasSuffix(template, "/") || strings.Contains(template, "//") || strings.ContainsAny(template, "?#") {
				return nil, fmt.Errorf("invalid API template: %s", template)
			}
			e := entry{route: route, operation: op, template: template, parts: strings.Split(strings.TrimPrefix(template, "/"), "/")}
			names := map[string]bool{}
			for _, part := range e.parts {
				if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") && len(part) > 2 {
					name := part[1 : len(part)-1]
					if names[name] || strings.ContainsAny(name, "{} ") {
						return nil, fmt.Errorf("invalid API parameter: %s", template)
					}
					names[name] = true
				} else {
					if part == "" || strings.ContainsAny(part, "{}") || part == "." || part == ".." {
						return nil, fmt.Errorf("invalid API segment: %s", template)
					}
					e.literals++
				}
			}
			if !route.Public && !names["sessionID"] {
				return nil, fmt.Errorf("missing session parameter: %s", template)
			}
			for _, previous := range r.entries {
				if !overlap(previous, e) {
					continue
				}
				if previous.literals == e.literals && (previous.template != e.template || previous.operation.Method == op.Method) {
					return nil, fmt.Errorf("ambiguous API operation: %s %s", op.Method, template)
				}
			}
			r.entries = append(r.entries, e)
		}
	}
	sort.SliceStable(r.entries, func(i, j int) bool { return r.entries[i].literals > r.entries[j].literals })
	return r, nil
}
func overlap(a, b entry) bool {
	if len(a.parts) != len(b.parts) {
		return false
	}
	for i, p := range a.parts {
		q := b.parts[i]
		if p != q && !strings.HasPrefix(p, "{") && !strings.HasPrefix(q, "{") {
			return false
		}
	}
	return true
}

type Match struct {
	Route         APIRoute
	Operation     Operation
	Template      string
	Params        map[string]string
	Rest          string
	Found         bool
	MethodAllowed bool
	Allow         []string
}

func (r *Registry) Match(method, path string) Match {
	// Historically both session-root spellings close the same authenticated session.
	if strings.HasPrefix(path, "/api/sessions/") && strings.Count(path, "/") == 4 && strings.HasSuffix(path, "/") {
		path = strings.TrimSuffix(path, "/")
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	var result Match
	specificity := -1
	for _, e := range r.entries {
		if specificity >= 0 && e.literals < specificity {
			break
		}
		if len(parts) != len(e.parts) {
			continue
		}
		matches := true
		for i, p := range e.parts {
			if parts[i] == "" || p != parts[i] && !strings.HasPrefix(p, "{") {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		if !result.Found {
			specificity = e.literals
			params := map[string]string{}
			for i, p := range e.parts {
				if strings.HasPrefix(p, "{") {
					params[p[1:len(p)-1]] = parts[i]
				}
			}
			rest := ""
			if e.operation.Suffix != "" {
				rest = strings.Join(parts[len(parts)-len(strings.Split(e.operation.Suffix, "/")):], "/")
			}
			result = Match{Route: e.route, Operation: e.operation, Template: e.template, Params: params, Rest: rest, Found: true}
		}
		result.Allow = append(result.Allow, e.operation.Method)
		if e.operation.Method == method {
			result.MethodAllowed = true
			result.Operation = e.operation
			result.Route = e.route
			if e.operation.Suffix == "" {
				result.Rest = ""
			} else {
				result.Rest = strings.Join(parts[len(parts)-len(strings.Split(e.operation.Suffix, "/")):], "/")
			}
		}
	}
	sort.Strings(result.Allow)
	// Match exposes execution metadata, not the registry's backing catalog slices.
	result.Route.Operations = nil
	result.Operation = cloneOperation(result.Operation)
	return result
}

func (m Match) Name(method string) string {
	switch method {
	case "GET", "HEAD", "POST", "PUT", "DELETE", "PATCH", "OPTIONS":
	default:
		method = "OTHER"
	}
	if !m.Found {
		return method + " unmatched"
	}
	return method + " " + m.Template
}
func (m Match) NeedsMutationLock() bool {
	return m.MethodAllowed && !m.Route.Public && m.Operation.Method != "GET" && m.Operation.Method != "HEAD" && !m.Operation.Long && m.Operation.Mutation != NoLock && m.Operation.Mutation != AfterDecode
}
func (m Match) MutationResource(query url.Values) string {
	switch m.Operation.Mutation {
	case IAMLock:
		return "iam"
	case BucketLock:
		if name := m.Params["name"]; name != "" {
			return "bucket/" + name
		}
		return "bucket-catalog"
	case AdminLock:
		return m.Route.Path
	}
	if bucket := query.Get("bucket"); bucket != "" {
		return "bucket/" + bucket
	}
	path := m.Route.Path
	if m.Rest != "" {
		path += "/" + m.Rest
	}
	return "operation/" + path
}

func validateResponses(op Operation) error {
	if op.Response != nil {
		if len(op.Responses) != 0 || op.Status != 0 && (op.Status < 200 || op.Status >= 300) {
			return fmt.Errorf("ambiguous or invalid success response")
		}
		return nil
	}
	if len(op.Responses) == 0 {
		return fmt.Errorf("missing explicit success response")
	}
	seen := map[string]bool{}
	for _, response := range op.Responses {
		if response.Status < 200 || response.Status >= 300 {
			return fmt.Errorf("invalid response status")
		}
		forms := 0
		if response.Model != nil {
			forms++
		}
		if len(response.Variants) > 0 {
			forms++
		}
		if response.Binary {
			forms++
		}
		if response.MediaType != "" && forms != 1 || response.MediaType == "" && forms != 0 {
			return fmt.Errorf("ambiguous response representation")
		}
		key := fmt.Sprint(response.Status) + " " + response.MediaType
		if seen[key] {
			return fmt.Errorf("duplicate response representation")
		}
		seen[key] = true
	}
	return nil
}
