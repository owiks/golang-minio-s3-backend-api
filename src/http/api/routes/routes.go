// Package routes defines the operation registry shared by dispatch and OpenAPI.
package routes

import (
	"net/http"
	"strings"

	"minio-web/api/src/services/runtime"
)

type Handler func(*runtime.State, http.ResponseWriter, *http.Request, *runtime.Session, string) error

// MutationScope identifies the owner of a mutation lock. Resource names decoded
// from bodies are locked by their use case, never before the body is validated.
type MutationScope uint8

const (
	OperationLock MutationScope = iota
	NoLock
	IAMLock
	BucketLock
	AdminLock
	AfterDecode
)

// Response describes an actual successful wire representation. Binary responses
// have no Go JSON model; NDJSON models describe one record, not the whole stream.
type Response struct {
	Status    int
	MediaType string
	Model     any
	Variants  []any
	Binary    bool
	// Localized marks other formats that contain API messages (ZIP manifests).
	// JSON and NDJSON language headers are documented automatically.
	Localized bool
}

type Operation struct {
	Handler       Handler
	Suffix        string
	Method        string
	Summary       string
	Controller    string
	Input         *Input
	Request       any
	BinaryRequest bool
	Response      any
	Responses     []Response
	Required      []string
	Status        int
	// Long selects the streaming budget and session transfer admission.
	Long              bool
	Mutation          MutationScope
	InvalidatesAccess bool
}

// Session routes use relative paths; public routes use absolute paths.
// Suffix templates are exact operations, never arbitrary trailing prefixes.
type APIRoute struct {
	Path       string
	Public     bool
	Tag        string
	Operations []Operation
}

func (r APIRoute) Template(op Operation) string {
	path := r.Path
	if !r.Public {
		path = "/api/sessions/{sessionID}"
		if r.Path != "" {
			path += "/" + r.Path
		}
	}
	if op.Suffix != "" {
		path += "/" + op.Suffix
	}
	return path
}

func IsDocumentation(path string) bool {
	return path == "/api/openapi.json" || path == "/api/docs" || strings.HasPrefix(path, "/api/docs/")
}
