package architecture

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"
	"testing"
)

func TestControllersUseSharedStreamingTransport(t *testing.T) {
	packages, lookup := loadExportedPackages(t, "./src/http/api/routes/...")
	checked, ndjson, interrupts := 0, 0, 0
	for _, p := range packages {
		if !strings.Contains(p.ImportPath, "/src/http/api/routes/") || !strings.HasSuffix(p.ImportPath, "/controllers") {
			continue
		}
		checked++
		fset := token.NewFileSet()
		var files []*ast.File
		for _, name := range p.GoFiles {
			file, err := parser.ParseFile(fset, filepath.Join(p.Dir, name), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, file)
		}
		violations, sends, stops := controllerStreamHelpers(t, fset, p.ImportPath, files, lookup)
		ndjson, interrupts = ndjson+sends, interrupts+stops
		for _, violation := range violations {
			t.Error("controller duplicates streaming transport", violation)
		}
	}
	if checked == 0 || ndjson < 2 || interrupts < 2 {
		t.Fatal("existing stream consumers were not checked", checked, ndjson, interrupts)
	}
	fset := token.NewFileSet()
	fixture, err := parser.ParseFile(fset, "fixture.go", `package controllers
import (
 . "encoding/json"
 ctx "context"
 http "net/http"
)
var _,_,_ = NewEncoder, ctx.AfterFunc, (*http.ResponseController).Flush
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	if violations, _, _ := controllerStreamHelpers(t, fset, "fixture/controllers", []*ast.File{fixture}, lookup); len(violations) != 3 {
		t.Fatal("aliases or function values bypassed streaming boundary", violations)
	}
}

func controllerStreamHelpers(t *testing.T, fset *token.FileSet, path string, files []*ast.File, lookup importer.Lookup) ([]string, int, int) {
	t.Helper()
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}}
	config := types.Config{Importer: importer.ForCompiler(fset, "gc", lookup)}
	if _, err := config.Check(path, fset, files, info); err != nil {
		t.Fatal(err)
	}
	var violations []string
	ndjson, interrupts := 0, 0
	for identifier, object := range info.Uses {
		function, ok := object.(*types.Func)
		if !ok || function.Pkg() == nil {
			continue
		}
		switch function.Pkg().Path() + "." + function.Name() {
		case "encoding/json.NewEncoder", "context.AfterFunc", "net/http.Flush":
			violations = append(violations, fset.Position(identifier.Pos()).String()+" "+function.FullName())
		case "github.com/owiks/golang-minio-s3-backend-api/src/http/stream.WriteNDJSON":
			ndjson++
		case "github.com/owiks/golang-minio-s3-backend-api/src/http/stream.InterruptOnCancel":
			interrupts++
		}
	}
	return violations, ndjson, interrupts
}
