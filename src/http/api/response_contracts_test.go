package api

import (
	"encoding/json"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/doc"
)

func TestResponseModelsAreSpecificToOperation(t *testing.T) {
	raw, err := json.Marshal(doc.Build(Catalog()))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Paths map[string]map[string]struct {
			Description string
			Responses   map[string]struct {
				Content map[string]struct{ Schema map[string]any }
			}
		}
	}
	if err = json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	prefix := "/api/sessions/{sessionID}/"
	get := document.Paths[prefix+"objects/content"]["get"].Responses["200"].Content["application/json"].Schema["properties"].(map[string]any)
	put := document.Paths[prefix+"objects/content"]["put"].Responses["200"].Content["application/json"].Schema["properties"].(map[string]any)
	if get["text"] == nil || get["ok"] != nil || put["ok"] == nil || put["text"] != nil {
		t.Fatal("read and edit responses mixed")
	}
	checksumGet := document.Paths[prefix+"objects/checksum"]["get"]
	checksumPost := document.Paths[prefix+"objects/checksum"]["post"]
	if checksumGet.Responses["200"].Content["application/json"].Schema["properties"] == nil || checksumGet.Description != "" || checksumPost.Responses["200"].Content["application/x-ndjson"].Schema["properties"] == nil {
		t.Fatal("JSON and stream contracts mixed")
	}
	download := document.Paths[prefix+"objects/archive/{ticket}"]["get"].Responses["200"].Content
	if len(download) != 1 || download["application/zip"].Schema["format"] != "binary" {
		t.Fatal("ZIP is not a JSON payload")
	}
	if len(document.Paths["/api/shared-folder/file/{ticket}"]["head"].Responses["200"].Content) != 0 {
		t.Fatal("HEAD must not document a body")
	}
	for path, methods := range document.Paths {
		for method, operation := range methods {
			for _, response := range operation.Responses {
				for media, content := range response.Content {
					if media == "application/json" && len(content.Schema) == 0 {
						t.Fatalf("untyped root response: %s %s", method, path)
					}
				}
			}
		}
	}
}
