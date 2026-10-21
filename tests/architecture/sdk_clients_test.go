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

func TestBusinessServicesDoNotConstructOrExposeSDKClients(t *testing.T) {
	packages, lookup := loadExportedPackages(t, "./src/services/...")
	checked := 0
	for _, p := range packages {
		if !strings.HasPrefix(p.ImportPath, "minio-web/api/src/services/") {
			continue
		}
		// Native SDK construction/transport adapters live exclusively in these two
		// infrastructure packages; business packages may use SDK DTOs and readers.
		if p.ImportPath == "minio-web/api/src/services/storage" || p.ImportPath == "minio-web/api/src/services/runtime" {
			continue
		}
		checked++
		t.Run(p.ImportPath, func(t *testing.T) {
			fset := token.NewFileSet()
			var files []*ast.File
			for _, name := range p.GoFiles {
				f, err := parser.ParseFile(fset, filepath.Join(p.Dir, name), nil, 0)
				if err != nil {
					t.Fatal(err)
				}
				files = append(files, f)
			}
			for _, v := range businessSDKClients(t, fset, p.ImportPath, files, lookup) {
				t.Error(v)
			}
		})
	}
	if checked == 0 {
		t.Fatal("no business packages checked")
	}
	t.Run("client aliases and factory methods are rejected", func(t *testing.T) {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "service.go", `package service
import (
 "context"
 "net/http"
 sdk "github.com/minio/minio-go/v7"
)
type native = sdk.Client
type Dependency interface{ Factory(http.RoundTripper)(*native,error) }
func Broken(ctx context.Context,c Dependency){client,_:=c.Factory(nil);_=sdk.Core{Client:client};client.StatObject(ctx,"bucket","key",sdk.StatObjectOptions{})}
`, 0)
		if err != nil {
			t.Fatal(err)
		}
		if violations := businessSDKClients(t, fset, "fixture/service", []*ast.File{file}, lookup); len(violations) < 3 {
			t.Fatal("SDK clients escaped isolation", violations)
		}
	})
}
func businessSDKClients(t *testing.T, fset *token.FileSet, path string, files []*ast.File, lookup importer.Lookup) []string {
	t.Helper()
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}}
	conf := types.Config{Importer: importer.ForCompiler(fset, "gc", lookup)}
	if _, err := conf.Check(path, fset, files, info); err != nil {
		t.Fatal(err)
	}
	var violations []string
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			expression, ok := node.(ast.Expr)
			if !ok {
				return true
			}
			switch expression.(type) {
			case *ast.SelectorExpr, *ast.CompositeLit:
			default:
				return true
			}
			value := info.TypeOf(expression)
			client := isSDKClient(value)
			if signature, ok := value.(*types.Signature); ok {
				if signature.Recv() != nil {
					client = client || isSDKClient(signature.Recv().Type())
				}
				for i := 0; i < signature.Results().Len(); i++ {
					client = client || isSDKClient(signature.Results().At(i).Type())
				}
			}
			if client {
				violations = append(violations, "concrete SDK client in business service: "+fset.Position(expression.Pos()).String())
			}
			return true
		})
	}
	return violations
}
