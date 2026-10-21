// Command openapi renders the explicit request/response operation registry.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"minio-web/api/src/http/api"
	"minio-web/api/src/http/api/doc"
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
