package localization

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nicksnyder/go-i18n/v2/i18n"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

func TestEmbeddedCatalogsCoverEveryMessage(t *testing.T) {
	catalog, err := New(catalogs)
	if err != nil {
		t.Fatal(err)
	}
	ids := messages.AllIDs()
	if len(ids) != len(catalog.parameters) {
		t.Fatalf("keys=%d catalog=%d", len(ids), len(catalog.parameters))
	}
	for _, lang := range []string{"en", "ru"} {
		data, err := fs.ReadFile(catalogs, "locales/active."+lang+".json")
		if err != nil {
			t.Fatal(err)
		}
		var entries []i18n.Message
		if err := json.Unmarshal(data, &entries); err != nil {
			t.Fatal(err)
		}
		if len(entries) != len(ids) {
			t.Fatalf("%s has missing translations", lang)
		}
		for _, id := range ids {
			names, exists := catalog.parameters[string(id)]
			if !exists {
				t.Fatalf("missing key %s", id)
			}
			counts := []int64{2}
			if slices.Contains(names, "Count") {
				counts = []int64{0, 1, 2, 5, 21, 101}
			}
			for _, count := range counts {
				args := make([]messages.Argument, 0, len(names))
				for _, name := range names {
					if name == "Count" {
						args = append(args, messages.Int(name, count))
					} else {
						args = append(args, messages.String(name, "fixture"))
					}
				}
				text := catalog.For(lang).Render(id.With(args...))
				if text == "" || text == "The API could not complete the request." || text == string(id) || strings.Contains(text, "<no value>") || strings.Contains(text, "{{") {
					t.Fatalf("%s/%s count=%d failed: %q", lang, id, count, text)
				}
			}
		}
	}
}
func testCatalog(en, ru string) fstest.MapFS {
	return fstest.MapFS{"locales/active.en.json": {Data: []byte(en)}, "locales/active.ru.json": {Data: []byte(ru)}}
}
func TestFallbackAndOpaqueData(t *testing.T) {
	c, err := New(testCatalog(`[{"id":"test","other":"Safe {{.Name}}"}]`, `[]`))
	if err != nil {
		t.Fatal(err)
	}
	r := messages.ID("test").With(messages.String("Name", "Файл http.internal_error"))
	if got := c.For("ru").Render(r); got != "Safe Файл http.internal_error" {
		t.Fatal(got)
	}
	if got := c.For("ru").Render(messages.Literal("http.internal_error Привет")); got != "http.internal_error Привет" {
		t.Fatal(got)
	}
	for _, bad := range []messages.Ref{messages.ID("missing-secret"), messages.ID("test"), messages.ID("test").With(messages.String("Name", "one"), messages.String("Name", "two"))} {
		if got := c.For("ru").Render(bad); got != "The API could not complete the request." {
			t.Fatal("unsafe fallback", got)
		}
	}
}
func TestCatalogRejectsInvalidInput(t *testing.T) {
	en := `[{"id":"test","other":"Safe {{.Name}}"}]`
	for name, files := range map[string]fstest.MapFS{
		"missing": {}, "malformed": testCatalog(`invalid-secret`, `[]`),
		"trailing": testCatalog(en+`{}`, `[]`), "unknown field": testCatalog(`[{"id":"test","other":"x","secret":true}]`, `[]`),
		"duplicate":            testCatalog(`[{"id":"test","other":"x"},{"id":"test","other":"y"}]`, `[]`),
		"empty text":           testCatalog(`[{"id":"test"}]`, `[]`),
		"bad template":         testCatalog(`[{"id":"test","other":"{{.Name"}]`, `[]`),
		"different parameters": testCatalog(en, `[{"id":"test","other":"{{.Other}}"}]`),
		"unknown ID":           testCatalog(en, `[{"id":"unknown","other":"x"}]`),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := New(files)
			if err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatal("catalog accepted or exposed input", err)
			}
		})
	}
}
func TestNestedMessagesAndPluralForms(t *testing.T) {
	c, err := New(testCatalog(`[{"id":"items","one":"{{.Count}} file","other":"{{.Count}} files"},{"id":"reason","other":"Reason: {{.Cause}}"}]`, `[{"id":"items","one":"{{.Count}} файл","few":"{{.Count}} файла","many":"{{.Count}} файлов","other":"{{.Count}} файла"},{"id":"reason","other":"Причина: {{.Cause}}"}]`))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		lang  string
		count int64
		want  string
	}{{"en", 1, "Reason: 1 file"}, {"en", 2, "Reason: 2 files"}, {"ru", 1, "Причина: 1 файл"}, {"ru", 2, "Причина: 2 файла"}, {"ru", 5, "Причина: 5 файлов"}} {
		ref := messages.ID("reason").With(messages.Text("Cause", messages.ID("items").With(messages.Int("Count", tc.count))))
		before := messages.Identity(ref)
		if got := c.For(tc.lang).Render(ref); got != tc.want {
			t.Fatalf("%s: %q", tc.lang, got)
		}
		if before != messages.Identity(ref) {
			t.Fatal("localization mutated description")
		}
	}
	// A missing field must not produce partial output or a template diagnostic.
	if text := c.For("ru").Render(messages.ID("items")); text != "The API could not complete the request." {
		t.Fatal(text)
	}
}
func TestCatalogParametersMatchDescription(t *testing.T) {
	// Used by the architecture check to keep parameterized call sites honest.
	names, err := templateParameters(`{{.Key}}: {{.Value}} {{if .Count}}{{.Count}}{{end}}`)
	if err != nil || !reflect.DeepEqual(names, []string{"Count", "Key", "Value"}) {
		t.Fatal(fmt.Sprint(names), err)
	}
}
