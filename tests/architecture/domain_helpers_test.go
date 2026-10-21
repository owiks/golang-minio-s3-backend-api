package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestDomainHelpersDoNotReturnToRuntime(t *testing.T) {
	root := filepath.Join("..", "..", "src", "services")
	for _, directory := range []string{"revision", "securetoken", "objects/state", "objects/write", "archives/plan", "archives/zipread", "folders/share", "uploads/invite", "iam"} {
		t.Run(directory, func(t *testing.T) {
			entries, err := os.ReadDir(filepath.Join(root, directory))
			if err != nil {
				t.Fatal(err)
			}
			checked := 0
			for _, entry := range entries {
				if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" {
					continue
				}
				file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, directory, entry.Name()), nil, 0)
				if err != nil {
					t.Fatal(err)
				}
				checked++
				for _, spec := range file.Imports {
					path, _ := strconv.Unquote(spec.Path.Value)
					if path == "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime" {
						t.Error("domain package depends on runtime", entry.Name())
					}
				}
			}
			if checked == 0 {
				t.Fatal("no domain files checked")
			}
		})
	}
	forbidden := map[string]bool{}
	for _, name := range []string{"Revision", "CanonicalPolicy", "DeletionSnapshot", "DeletionRevision", "ArchiveItem", "ArchivePlan", "ArchiveSnapshot", "FolderShare", "FolderShareSummary", "FolderFileTicket", "BeginFolderRead", "UploadInvite", "UploadInviteSummary", "ActionCheckIssue", "ActionReadIssue", "ActionAccountIssue", "CheckedZIP", "LoadStoredZIP", "RandomToken"} {
		forbidden[name] = true
	}
	entries, err := os.ReadDir(filepath.Join(root, "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "runtime", entry.Name()), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			switch value := decl.(type) {
			case *ast.FuncDecl:
				if value.Recv == nil && forbidden[value.Name.Name] {
					t.Error("domain helper or forwarding wrapper in runtime", entry.Name(), value.Name)
				}
			case *ast.GenDecl:
				for _, spec := range value.Specs {
					if typ, ok := spec.(*ast.TypeSpec); ok && forbidden[typ.Name.Name] {
						t.Error("domain model or compatibility alias in runtime", entry.Name(), typ.Name)
					}
				}
			}
		}
	}
}
