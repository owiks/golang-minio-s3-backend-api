package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Session connection is the composition boundary. All other business packages
// consume domain-owned ports; aliases and dot imports cannot hide runtime stores.
func runtimeDependency(owner string, file *ast.File) bool {
	if owner == "runtime" || owner == "sessions" {
		return false
	}
	for _, spec := range file.Imports {
		path, _ := strconv.Unquote(spec.Path.Value)
		if path == "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime" {
			return true
		}
	}
	return false
}

func TestBusinessPackagesDoNotDependOnRuntimeStores(t *testing.T) {
	root := filepath.Join("..", "..", "src", "services")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		owner, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		if runtimeDependency(filepath.ToSlash(owner), file) {
			t.Errorf("business package imports runtime instead of consumer-owned ports: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range []string{`import "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"`, `import container "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"`, `import . "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"`} {
		file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", "package example\n"+declaration, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !runtimeDependency("archives", file) || !runtimeDependency("folders/nested", file) {
			t.Fatal("runtime import escaped dependency check", declaration)
		}
		if runtimeDependency("sessions", file) || runtimeDependency("runtime", file) {
			t.Fatal("composition boundary was rejected")
		}
	}
}
