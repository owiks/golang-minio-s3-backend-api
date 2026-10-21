package architecture

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/uploads"
)

func TestImportDependsOnSourcePortInsteadOfHTTPClient(t *testing.T) {
	port := reflect.TypeFor[uploads.RemoteSource]()
	if port.Kind() != reflect.Interface || port.NumMethod() != 1 || port.Method(0).Name != "Open" {
		t.Fatal("remote source port became broad")
	}
	if reflect.TypeOf(uploads.Import).In(2) != port || reflect.TypeOf(uploads.StageRemote).In(1) != port {
		t.Fatal("import bypasses its source port")
	}
	for _, name := range []string{"import_usecase.go", "stage_remote.go", "dependencies.go"} {
		path := filepath.Join("..", "..", "src", "services", "uploads", name)
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range file.Imports {
			dependency, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if dependency == "net/http" || dependency == "net" || dependency == "crypto/tls" || dependency == "github.com/owiks/golang-minio-s3-backend-api/src/services/remotesource" {
				t.Errorf("import scenario depends on outgoing transport: %s -> %s", name, dependency)
			}
		}
	}
}
