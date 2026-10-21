package architecture

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"minio-web/api/src/services/search"
)

func TestFileSearchReceivesDecodedInputAndPreparesFiltersInService(t *testing.T) {
	input := reflect.TypeFor[search.FileSearchInput]()
	if reflect.TypeOf(search.SearchFiles).In(2) != input {
		t.Fatal("search can bypass decoded input")
	}
	for i := range input.NumField() {
		if input.Field(i).Type.Kind() != reflect.String {
			t.Fatal("input contains a compiled filter or transport type")
		}
	}
	path := filepath.Join("..", "..", "src", "http", "api", "routes", "search", "controllers", "file_search.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range file.Imports {
		name, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		if name == "strconv" || name == "time" || name == "strings" {
			t.Errorf("search filter preparation returned to HTTP: %s", name)
		}
	}
}
