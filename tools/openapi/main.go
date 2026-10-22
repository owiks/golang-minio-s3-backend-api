// Command openapi renders the explicit request/response operation registry.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/doc"
)

func main() {
	check := flag.Bool("check", false, "verify generated documentation without modifying it")
	flag.Parse()
	root, err := os.Getwd()
	must(err)
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			panic("run within the API module")
		}
		root = parent
	}
	raw, err := json.MarshalIndent(doc.Build(api.Catalog()), "", "  ")
	must(err)
	specification, err := openapi3.NewLoader().LoadFromData(raw)
	must(err)
	must(specification.Validate(context.Background()))
	raw = append(raw, '\n')
	path := filepath.Join(root, "swagger.generated.json")
	if *check {
		stored, err := os.ReadFile(path)
		must(err)
		if !bytes.Equal(raw, stored) {
			panic("generated Swagger is stale; run make docs")
		}
	} else {
		must(os.WriteFile(path, raw, 0644))
	}
	fmt.Println("Explicit OpenAPI operation contracts verified")
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
