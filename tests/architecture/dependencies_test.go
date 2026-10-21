package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

func TestRuntimeCollectionsHavePrivateOwners(t *testing.T) {
	state := reflect.TypeOf(runtime.State{})
	for i := range state.NumField() {
		field := state.Field(i)
		if field.Type.Kind() == reflect.Map || field.Type == reflect.TypeOf(sync.Mutex{}) || field.Type == reflect.TypeOf(sync.RWMutex{}) {
			t.Errorf("State owns collection storage or a shared mutex: %s", field.Name)
		}
	}
	for _, owner := range []reflect.Type{
		reflect.TypeOf(runtime.SessionStore{}), reflect.TypeOf(runtime.ArchiveStore{}),
		reflect.TypeOf(runtime.UploadInviteStore{}), reflect.TypeOf(runtime.FolderShareStore{}),
		reflect.TypeOf(runtime.ZIPCache{}), reflect.TypeOf(runtime.AccessRevisions{}),
	} {
		for i := range owner.NumField() {
			field := owner.Field(i)
			if field.PkgPath == "" {
				t.Errorf("store exposes mutable state: %s.%s", owner.Name(), field.Name)
			}
		}
	}
}

func TestLayerBoundaries(t *testing.T) {
	root := filepath.Join("..", "..", "src")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		owner := filepath.ToSlash(relative)
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			dependency, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if strings.Contains(dependency, "/internal/httpapi") || strings.Contains(dependency, "/src/cmd") && owner != "." {
				t.Errorf("invalid dependency: %s -> %s", owner, dependency)
			}
			if strings.HasPrefix(owner, "services/") && (strings.HasPrefix(dependency, "github.com/owiks/golang-minio-s3-backend-api/src/http") || strings.HasPrefix(dependency, "github.com/owiks/golang-minio-s3-backend-api/src/cmd")) {
				t.Errorf("service imports transport or command: %s -> %s", owner, dependency)
			}
			if strings.HasPrefix(owner, "http/") && strings.Contains(dependency, "/tests/") {
				t.Errorf("production imports tests: %s", path)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok || owner == "config" {
				return true
			}
			name, ok := selector.X.(*ast.Ident)
			if ok && name.Name == "os" && (selector.Sel.Name == "Getenv" || selector.Sel.Name == "LookupEnv" || selector.Sel.Name == "Environ") {
				t.Errorf("configuration read outside config: %s", path)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// SDK RoundTrippers are HTTP adapters. Business entry points must receive
// decoded values and a context, not the incoming request or its query map.
func TestServicesReceiveDecodedInputs(t *testing.T) {
	root := filepath.Join("..", "..", "src", "services")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		imports := map[string]string{}
		for _, spec := range file.Imports {
			dependency, _ := strconv.Unquote(spec.Path.Value)
			name := filepath.Base(dependency)
			if spec.Name != nil {
				name = spec.Name.Name
			}
			imports[name] = dependency
		}
		for _, declaration := range file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Name.Name == "RoundTrip" || outgoingSignerAdapter(path, fn) {
				continue
			}
			for _, parameter := range fn.Type.Params.List {
				ast.Inspect(parameter.Type, func(node ast.Node) bool {
					selector, ok := node.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					owner, ok := selector.X.(*ast.Ident)
					if !ok {
						return true
					}
					transportType := imports[owner.Name] == "net/http" && (selector.Sel.Name == "Request" || selector.Sel.Name == "ResponseWriter")
					queryMap := imports[owner.Name] == "net/url" && selector.Sel.Name == "Values"
					if transportType || queryMap {
						t.Errorf("HTTP input in service %s: %s", path, fn.Name.Name)
					}
					return true
				})
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			selector, ok := literal.Type.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			owner, ok := selector.X.(*ast.Ident)
			if ok && imports[owner.Name] == "net/http" && selector.Sel.Name == "Request" {
				t.Errorf("synthetic HTTP request in service: %s", path)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// This exact infrastructure method forwards SDK signing parameters; it does not
// parse incoming HTTP queries. Keep the exception out of business services.
func outgoingSignerAdapter(path string, fn *ast.FuncDecl) bool {
	if filepath.ToSlash(path) != "../../src/services/runtime/service_clients.go" || fn.Name.Name != "PresignedGetObject" || fn.Recv == nil || len(fn.Recv.List) != 1 {
		return false
	}
	pointer, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	name, ok := pointer.X.(*ast.Ident)
	return ok && name.Name == "Clients"
}

func TestBusinessServicesReceiveSpecificDependencies(t *testing.T) {
	root := filepath.Join("..", "..", "src", "services")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || filepath.Base(filepath.Dir(path)) == "runtime" {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		aliases := map[string]bool{}
		for _, spec := range file.Imports {
			dependency, _ := strconv.Unquote(spec.Path.Value)
			if dependency != "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime" {
				continue
			}
			name := "runtime"
			if spec.Name != nil {
				name = spec.Name.Name
			}
			aliases[name] = true
		}
		check := func(fields *ast.FieldList) {
			if fields == nil {
				return
			}
			for _, field := range fields.List {
				ast.Inspect(field.Type, func(node ast.Node) bool {
					selector, ok := node.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					owner, ok := selector.X.(*ast.Ident)
					if ok && aliases[owner.Name] && (selector.Sel.Name == "State" || selector.Sel.Name == "Session" || selector.Sel.Name == "Clients") {
						t.Errorf("business service depends on runtime container %s in %s", selector.Sel.Name, path)
					}
					return true
				})
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch value := node.(type) {
			case *ast.FuncDecl:
				check(value.Type.Params)
			case *ast.StructType:
				check(value.Fields)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Business scenarios expose named results or deliberately closed unions. Keep
// arbitrary JSON in explicit policy values, not in untyped scenario signatures.
func TestUseCasesHaveTypedResults(t *testing.T) {
	root := filepath.Join("..", "..", "src", "services")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, "_usecase.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Type.Results == nil {
				continue
			}
			for _, field := range fn.Type.Results.List {
				ast.Inspect(field.Type, func(node ast.Node) bool {
					if id, ok := node.(*ast.Ident); ok && id.Name == "any" {
						t.Errorf("untyped result: %s %s", path, fn.Name.Name)
					}
					if value, ok := node.(*ast.InterfaceType); ok && len(value.Methods.List) == 0 {
						t.Errorf("empty interface result: %s %s", path, fn.Name.Name)
					}
					return true
				})
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
