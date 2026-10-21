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

func TestConditionalWritesAreIndependentOfUploadUseCases(t *testing.T) {
	root := filepath.Join("..", "..", "src", "services")
	for _, domain := range []string{"objects", "transfers", "archives"} {
		err := filepath.WalkDir(filepath.Join(root, domain), func(path string, entry fs.DirEntry, err error) error {
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
			for _, spec := range file.Imports {
				importPath, _ := strconv.Unquote(spec.Path.Value)
				if importPath == "minio-web/api/src/services/uploads" {
					t.Error("object operation depends on upload use cases", path)
				}
				if filepath.Dir(path) == filepath.Join(root, "objects", "write") && strings.HasPrefix(importPath, "minio-web/api/src/services/") && importPath != "minio-web/api/src/services/apperror" && importPath != "minio-web/api/src/services/objects/state" {
					t.Error("conditional write mechanism imports a use case", path, importPath)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// Moving the implementation must not leave forwarding helpers or aliases.
	files, err := parser.ParseDir(token.NewFileSet(), filepath.Join(root, "uploads"), func(info fs.FileInfo) bool { return !strings.HasSuffix(info.Name(), "_test.go") }, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range files {
		for name, file := range pkg.Files {
			for _, declaration := range file.Decls {
				var names []string
				switch value := declaration.(type) {
				case *ast.FuncDecl:
					if value.Recv == nil {
						names = append(names, value.Name.Name)
					}
				case *ast.GenDecl:
					for _, spec := range value.Specs {
						if typ, ok := spec.(*ast.TypeSpec); ok {
							names = append(names, typ.Name.Name)
						}
					}
				}
				for _, symbol := range names {
					switch symbol {
					case "FileState", "DestinationStateClient", "DestinationState", "WriteOptions", "FileWriteError":
						t.Error("shared write implementation or alias returned to uploads", name, symbol)
					}
				}
			}
		}
	}
}
