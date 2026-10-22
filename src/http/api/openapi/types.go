package openapi

type Schema map[string]any

type Parameter struct {
	Name        string `json:"name"`
	In          string `json:"in"`
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description,omitempty"`
	Schema      Schema `json:"schema"`
}

type Operation struct {
	APIScopes   []string              `json:"x-api-scopes,omitempty"`
	OperationID string                `json:"operationId"`
	Tags        []string              `json:"tags"`
	Summary     string                `json:"summary"`
	Description string                `json:"description,omitempty"`
	Parameters  []Parameter           `json:"parameters,omitempty"`
	RequestBody any                   `json:"requestBody,omitempty"`
	Responses   map[string]any        `json:"responses"`
	Security    []map[string][]string `json:"security"`
}

type Document struct {
	OpenAPI    string                          `json:"openapi"`
	Info       map[string]string               `json:"info"`
	Servers    []map[string]string             `json:"servers"`
	Paths      map[string]map[string]Operation `json:"paths"`
	Components map[string]any                  `json:"components"`
}
