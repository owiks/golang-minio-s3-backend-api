package architecture

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type exportedPackage struct {
	ImportPath, Dir, Export string
	GoFiles                 []string
}

func loadExportedPackages(t *testing.T, pattern string) ([]exportedPackage, importer.Lookup) {
	t.Helper()
	// Use the compiler's export data, not selector spellings. This catches aliases,
	// embedded methods, bound method values and local SDK clients without adding
	// an analyzer dependency to the application.
	cmd := exec.Command("go", "list", "-deps", "-export", "-json", pattern)
	cmd.Dir = filepath.Join("..", "..")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("load %s export data: %v\n%s", pattern, err, stderr.String())
	}
	exports := map[string]string{}
	var packages []exportedPackage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	for {
		var p exportedPackage
		if err := decoder.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		exports[p.ImportPath] = p.Export
		packages = append(packages, p)
	}
	lookup := func(path string) (io.ReadCloser, error) {
		export := exports[path]
		if export == "" {
			return nil, fmt.Errorf("missing export data for %s", path)
		}
		return os.Open(export)
	}
	return packages, lookup
}

func TestControllersDelegateSDKOperations(t *testing.T) {
	packages, lookup := loadExportedPackages(t, "./src/http/api/routes/...")
	checked := 0
	for _, p := range packages {
		if !strings.Contains(p.ImportPath, "/src/http/api/routes/") || !strings.HasSuffix(p.ImportPath, "/controllers") {
			continue
		}
		checked++
		t.Run(p.ImportPath, func(t *testing.T) {
			fset := token.NewFileSet()
			var files []*ast.File
			for _, name := range p.GoFiles {
				file, err := parser.ParseFile(fset, filepath.Join(p.Dir, name), nil, 0)
				if err != nil {
					t.Fatal(err)
				}
				files = append(files, file)
			}
			for _, v := range checkControllerSDK(t, fset, p.ImportPath, files, lookup) {
				t.Errorf("SDK operation in controller: %s", v)
			}
		})
	}
	if checked == 0 {
		t.Fatal("no controller packages checked")
	}
	t.Run("aliases and method values cannot bypass boundary", func(t *testing.T) {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "controller.go", `package controllers
import (
 "context"
 sdk "github.com/minio/minio-go/v7"
 native "github.com/minio/madmin-go/v3"
 connection "minio-web/api/src/services/runtime"
)
type wrapper struct {*sdk.Client}
func delegate(*native.AdminClient) {}
func Handle(s *connection.Session,c *sdk.Client) {
 local:=connection.NewClients(s)
 local.GetLogs(context.Background(),"",1,"ALL")
 bound:=local.GetLogs; _=bound
 factory:=native.New; _,_=factory("fixture.example","key","secret",false)
 method:=(*sdk.Client).StatObject; _=method
 constructor:=native.NewWithOptions; _=constructor
 embedded:=wrapper{c};embedded.StatObject(context.Background(),"bucket","key",sdk.StatObjectOptions{})
 delegate(local.AdminClient.AdminClient)
}
`, 0)
		if err != nil {
			t.Fatal(err)
		}
		violations := checkControllerSDK(t, fset, "fixture/controllers", []*ast.File{file}, lookup)
		if len(violations) != 6 {
			t.Fatalf("expected six real SDK uses, got %v", violations)
		}
	})
	t.Run("passing dependencies to a service is allowed", func(t *testing.T) {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "controller.go", `package controllers
import (
 sdk "minio-web/api/src/services/storage"
 connection "minio-web/api/src/services/runtime"
)
func delegate(*sdk.AdminClient) {}
func Handle(s *connection.Session) { delegate(connection.NewClients(s).AdminClient); _=connection.NewClients(s) }
`, 0)
		if err != nil {
			t.Fatal(err)
		}
		if v := checkControllerSDK(t, fset, "fixture/controllers", []*ast.File{file}, lookup); len(v) != 0 {
			t.Fatal(v)
		}
	})
	t.Run("guarded admin commands still belong to services", func(t *testing.T) {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "controller.go", `package controllers
import (
 "context"
 connection "minio-web/api/src/services/runtime"
)
func Handle(s *connection.Session) {
 c:=connection.NewClients(s)
 c.StartBatchJob(context.Background(),"fixture")
 command:=c.StartBatchJob; _=command
}
`, 0)
		if err != nil {
			t.Fatal(err)
		}
		if violations := checkControllerSDK(t, fset, "fixture/controllers", []*ast.File{file}, lookup); len(violations) != 2 {
			t.Fatalf("guarded commands bypass controller boundary: %v", violations)
		}
	})
}

func checkControllerSDK(t *testing.T, fset *token.FileSet, path string, files []*ast.File, lookup importer.Lookup) []string {
	t.Helper()
	info := &types.Info{Selections: map[*ast.SelectorExpr]*types.Selection{}, Uses: map[*ast.Ident]types.Object{}}
	conf := types.Config{Importer: importer.ForCompiler(fset, "gc", lookup)}
	if _, err := conf.Check(path, fset, files, info); err != nil {
		t.Fatal(err)
	}
	var violations []string
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			selector, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			function, ok := info.Uses[selector.Sel].(*types.Func)
			if !ok {
				return true
			} // Field/type references may be passed to a service.
			sdk := isStorageSDK(function.Pkg())
			if signature, ok := function.Type().(*types.Signature); ok {
				if signature.Recv() != nil {
					sdk = sdk || isSDKClient(signature.Recv().Type())
				}
				for i := 0; i < signature.Results().Len(); i++ {
					sdk = sdk || isSDKClient(signature.Results().At(i).Type())
				}
			}
			if sdk {
				violations = append(violations, fset.Position(selector.Pos()).String()+" "+selector.Sel.Name)
			}
			return true
		})
	}
	return violations
}

func isStorageSDK(pkg *types.Package) bool {
	if pkg == nil {
		return false
	}
	for _, prefix := range []string{"github.com/minio/minio-go/v7", "github.com/minio/madmin-go/v3"} {
		if pkg.Path() == prefix || strings.HasPrefix(pkg.Path(), prefix+"/") {
			return true
		}
	}
	return false
}
func isSDKClient(value types.Type) bool {
	value = types.Unalias(value)
	if pointer, ok := value.(*types.Pointer); ok {
		value = types.Unalias(pointer.Elem())
	}
	named, ok := value.(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}
	if named.Obj().Pkg().Path() == "minio-web/api/src/services/storage" && named.Obj().Name() == "AdminClient" {
		return true
	}
	if !isStorageSDK(named.Obj().Pkg()) {
		return false
	}
	switch named.Obj().Name() {
	case "Client", "Core", "AdminClient", "MetricsClient":
		return true
	}
	return false
}
