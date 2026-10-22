package routes

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
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
	paths   []pathTier
	catalog []APIRoute
}

// Paths are resolved independently of methods so a wrong-method literal route
// cannot fall through to a less specific parameter route. Separate chi trees
// retain the catalog's total-literal priority, rather than chi's segment priority.
type pathTier struct {
	router *chi.Mux
	routes map[string][]entry
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
	var entries []entry
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
			if op.Operator && (!route.Public || op.Method != http.MethodGet || !strings.HasPrefix(template, "/api/operator/") || len(op.Scopes) != 0) {
				return nil, fmt.Errorf("invalid operator operation: %s", template)
			}
			if strings.HasPrefix(template, "/api/operator/") && !op.Operator {
				return nil, fmt.Errorf("operator operation without authentication: %s", template)
			}
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
			for _, previous := range entries {
				if !overlap(previous, e) {
					continue
				}
				if previous.literals == e.literals && (previous.template != e.template || previous.operation.Method == op.Method) {
					return nil, fmt.Errorf("ambiguous API operation: %s %s", op.Method, template)
				}
			}
			entries = append(entries, e)
		}
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].literals > entries[j].literals })
	for i, e := range entries {
		if i == 0 || entries[i-1].literals != e.literals {
			r.paths = append(r.paths, pathTier{router: chi.NewRouter(), routes: make(map[string][]entry)})
		}
		tier := &r.paths[len(r.paths)-1]
		pattern := chiPattern(e.parts)
		if _, exists := tier.routes[pattern]; !exists {
			// Find, not ServeHTTP, is used below. The execution pipeline remains
			// responsible for authentication, method errors and controller budgets.
			tier.router.Get(pattern, http.NotFound)
		}
		tier.routes[pattern] = append(tier.routes[pattern], e)
	}
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

// Internal parameter names prevent catalog parameter names from introducing
// chi regular expressions. A literal asterisk must also remain literal.
func chiPattern(parts []string) string {
	pattern := make([]string, len(parts))
	for i, part := range parts {
		switch {
		case strings.HasPrefix(part, "{"):
			pattern[i] = "{p" + strconv.Itoa(i) + "}"
		case strings.Contains(part, "*"):
			// Explicit anchors avoid chi mistaking an escaped literal '$' for
			// the regexp's end anchor when it normalizes custom patterns.
			pattern[i] = "{l" + strconv.Itoa(i) + ":^(?:" + regexp.QuoteMeta(part) + ")$}"
		default:
			pattern[i] = part
		}
	}
	return "/" + strings.Join(pattern, "/")
}

func (r *Registry) Match(method, path string) Match {
	// Historically both session-root spellings close the same authenticated session.
	if strings.HasPrefix(path, "/api/sessions/") && strings.Count(path, "/") == 4 && strings.HasSuffix(path, "/") {
		path = strings.TrimSuffix(path, "/")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	for _, tier := range r.paths {
		ctx := chi.NewRouteContext()
		pattern := tier.router.Find(ctx, http.MethodGet, path)
		if pattern == "" {
			continue
		}
		entries := tier.routes[pattern]
		first := entries[0]
		params := make(map[string]string)
		for i, part := range first.parts {
			if strings.HasPrefix(part, "{") {
				value := ctx.URLParam("p" + strconv.Itoa(i))
				if value == "" {
					return Match{}
				}
				params[part[1:len(part)-1]] = value
			}
		}
		result := Match{Route: first.route, Operation: first.operation, Template: first.template, Params: params, Found: true}
		for _, e := range entries {
			result.Allow = append(result.Allow, e.operation.Method)
			if e.operation.Method == method {
				result.MethodAllowed = true
				result.Operation = e.operation
				result.Route = e.route
			}
		}
		if suffix := result.Operation.Suffix; suffix != "" {
			parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
			result.Rest = strings.Join(parts[len(parts)-len(strings.Split(suffix, "/")):], "/")
		}
		sort.Strings(result.Allow)
		// Match exposes execution metadata, not the registry's backing catalog slices.
		result.Route.Operations = nil
		result.Operation = cloneOperation(result.Operation)
		return result
	}
	return Match{}
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
