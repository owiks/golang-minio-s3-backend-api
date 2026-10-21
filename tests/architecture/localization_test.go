package architecture

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestMessageParametersMatchCatalogAndLocalizationStaysAtHTTPBoundary(t *testing.T) {
	root := filepath.Join("..", "..", "src")
	var catalog []struct{ ID, Other string }
	raw, err := os.ReadFile(filepath.Join(root, "services/localization/locales/active.en.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	parameters := map[string][]string{}
	placeholder := regexp.MustCompile(`\{\{\.([A-Za-z]+)\}\}`)
	for _, entry := range catalog {
		for _, match := range placeholder.FindAllStringSubmatch(entry.Other, -1) {
			parameters[entry.ID] = append(parameters[entry.ID], match[1])
		}
		slices.Sort(parameters[entry.ID])
		parameters[entry.ID] = slices.Compact(parameters[entry.ID])
	}
	keys, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "services/messages/keys.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{"Combined": "common.combined"}
	ast.Inspect(keys, func(n ast.Node) bool {
		v, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range v.Names {
			if i < len(v.Values) {
				if lit, ok := v.Values[i].(*ast.BasicLit); ok {
					ids[name.Name], _ = strconv.Unquote(lit.Value)
				}
			}
		}
		return true
	})
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
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
		parents := map[ast.Node]ast.Node{}
		var stack []ast.Node
		ast.Inspect(file, func(node ast.Node) bool {
			if node == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			if len(stack) > 0 {
				parents[node] = stack[len(stack)-1]
			}
			stack = append(stack, node)
			return true
		})
		for _, imp := range file.Imports {
			dependency, _ := strconv.Unquote(imp.Path.Value)
			if strings.Contains(path, "/services/") && !strings.Contains(path, "/services/localization/") && (dependency == "github.com/owiks/golang-minio-s3-backend-api/src/services/localization" || strings.HasPrefix(dependency, "github.com/nicksnyder/go-i18n")) {
				t.Error("service chooses a language", path)
			}
			if strings.Contains(path, "/http/presentation/") && (dependency == "reflect" || dependency == "regexp") {
				t.Error("response projection scans arbitrary data", path)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if key, ok := node.(*ast.SelectorExpr); ok {
				if pkg, ok := key.X.(*ast.Ident); ok && pkg.Name == "messages" {
					if id, exists := ids[key.Sel.Name]; exists && len(parameters[id]) > 0 {
						parent, parameterized := parents[key].(*ast.SelectorExpr)
						if !parameterized || parent.X != key || parent.Sel.Name != "With" {
							t.Error("parameterized message used without arguments", path, id)
						}
					}
				}
			}
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "With" {
				return true
			}
			key, ok := sel.X.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := key.X.(*ast.Ident)
			if !ok || pkg.Name != "messages" {
				return true
			}
			id, ok := ids[key.Sel.Name]
			if !ok {
				t.Error("unknown message key", path, key.Sel.Name)
				return true
			}
			var actual []string
			for _, arg := range call.Args {
				value, ok := arg.(*ast.CallExpr)
				if !ok || len(value.Args) == 0 {
					t.Error("opaque message parameters", path, id)
					continue
				}
				name, ok := value.Args[0].(*ast.BasicLit)
				if !ok {
					t.Error("dynamic parameter name", path, id)
					continue
				}
				text, _ := strconv.Unquote(name.Value)
				actual = append(actual, text)
			}
			slices.Sort(actual)
			if !slices.Equal(actual, parameters[id]) {
				t.Errorf("%s: %s args=%v want=%v", path, id, actual, parameters[id])
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
