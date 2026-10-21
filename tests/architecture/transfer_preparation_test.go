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

func TestTransferControllersInvokeUseCasesOnly(t *testing.T) {
	packages, lookup := loadExportedPackages(t, "./src/http/api/routes/transfers/controllers")
	checked := 0
	for _, p := range packages {
		if !strings.HasSuffix(p.ImportPath, "/routes/transfers/controllers") {
			continue
		}
		fset := token.NewFileSet()
		var files []*ast.File
		for _, name := range p.GoFiles {
			file, err := parser.ParseFile(fset, filepath.Join(p.Dir, name), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, file)
		}
		checked++
		for _, violation := range transferControllerHelpers(t, fset, p.ImportPath, files, lookup) {
			t.Error("transfer preparation logic returned to HTTP", violation)
		}
	}
	if checked != 1 {
		t.Fatal("transfer controllers not checked")
	}
	// Function values and dot imports must not bypass the use-case boundary.
	fset := token.NewFileSet()
	fixture, err := parser.ParseFile(fset, "fixture.go", `package controllers
import (
 . "github.com/owiks/golang-minio-s3-backend-api/src/services/transfers"
 folders "github.com/owiks/golang-minio-s3-backend-api/src/services/folders"
 write "github.com/owiks/golang-minio-s3-backend-api/src/services/objects/write"
 app "github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
 runtime "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)
var _,_,_,_,_ = PrepareFolderTransfer, PrepareDestination, Transfer, runtime.NewClients, app.New
var _,_,_,_ = ValidTransferKey, folders.FolderDeleteItems, write.DestinationState, app.Public
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	if violations := transferControllerHelpers(t, fset, "fixture/controllers", []*ast.File{fixture}, lookup); len(violations) != 4 {
		t.Fatal("helpers escaped boundary or legitimate use cases rejected", violations)
	}
}

func transferControllerHelpers(t *testing.T, fset *token.FileSet, path string, files []*ast.File, lookup importer.Lookup) []string {
	t.Helper()
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}}
	config := types.Config{Importer: importer.ForCompiler(fset, "gc", lookup)}
	if _, err := config.Check(path, fset, files, info); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]map[string]bool{
		"github.com/owiks/golang-minio-s3-backend-api/src/services/transfers": {"PrepareFolderTransfer": true, "PrepareDestination": true, "Transfer": true},
		"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime":   {"NewClients": true},
		"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror":  {"New": true},
	}
	var violations []string
	for identifier, object := range info.Uses {
		function, ok := object.(*types.Func)
		if !ok || function.Pkg() == nil || !strings.HasPrefix(function.Pkg().Path(), "github.com/owiks/golang-minio-s3-backend-api/src/services/") {
			continue
		}
		if !allowed[function.Pkg().Path()][function.Name()] {
			violations = append(violations, fset.Position(identifier.Pos()).String()+" "+function.FullName())
		}
	}
	return violations
}
