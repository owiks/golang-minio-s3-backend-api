// Package localization adapts immutable message descriptions to go-i18n.
package localization

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"sync"
	"text/template"
	"text/template/parse"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

//go:embed locales/active.*.json
var catalogs embed.FS

type Catalog struct {
	bundle     *i18n.Bundle
	parameters map[string][]string
}

// New loads a private bundle. It is never modified after publication.
func New(files fs.FS) (*Catalog, error) {
	bundle := i18n.NewBundle(language.English)
	parameters := map[string][]string{}
	for _, lang := range []string{"en", "ru"} {
		name := "locales/active." + lang + ".json"
		data, err := fs.ReadFile(files, name)
		if err != nil {
			return nil, errors.New("cannot read API translations")
		}
		var entries []i18n.Message
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&entries) != nil || decoder.Decode(new(any)) != io.EOF {
			return nil, errors.New("invalid API translation catalog")
		}
		seen := map[string]bool{}
		for _, entry := range entries {
			if entry.ID == "" || seen[entry.ID] {
				return nil, errors.New("invalid or duplicate API message ID")
			}
			seen[entry.ID] = true
			if entry.Other == "" {
				return nil, errors.New("API message has no fallback form")
			}
			var required []string
			for _, form := range []string{entry.Zero, entry.One, entry.Two, entry.Few, entry.Many, entry.Other} {
				if form == "" {
					continue
				}
				names, err := templateParameters(form)
				if err != nil {
					return nil, errors.New("invalid API translation template")
				}
				required = append(required, names...)
			}
			slices.Sort(required)
			required = slices.Compact(required)
			if lang == "en" {
				parameters[entry.ID] = required
			} else if source, ok := parameters[entry.ID]; !ok || !slices.Equal(source, required) {
				return nil, errors.New("API translation parameters differ from English")
			}
		}
		if _, err := bundle.ParseMessageFileBytes(data, "active."+lang+".json"); err != nil {
			return nil, errors.New("cannot parse API translations")
		}
	}
	return &Catalog{bundle: bundle, parameters: parameters}, nil
}

func templateParameters(text string) ([]string, error) {
	t, err := template.New("message").Parse(text)
	if err != nil {
		return nil, err
	}
	var names []string
	var walk func(parse.Node)
	walk = func(node parse.Node) {
		if node == nil {
			return
		}
		switch n := node.(type) {
		case *parse.ListNode:
			if n != nil {
				for _, child := range n.Nodes {
					walk(child)
				}
			}
		case *parse.ActionNode:
			walk(n.Pipe)
		case *parse.PipeNode:
			for _, command := range n.Cmds {
				walk(command)
			}
		case *parse.CommandNode:
			for _, argument := range n.Args {
				walk(argument)
			}
		case *parse.FieldNode:
			if len(n.Ident) > 0 {
				names = append(names, n.Ident[0])
			}
		case *parse.IfNode:
			walk(n.Pipe)
			walk(n.List)
			walk(n.ElseList)
		case *parse.RangeNode:
			walk(n.Pipe)
			walk(n.List)
			walk(n.ElseList)
		case *parse.WithNode:
			walk(n.Pipe)
			walk(n.List)
			walk(n.ElseList)
		}
	}
	walk(t.Tree.Root)
	slices.Sort(names)
	return slices.Compact(names), nil
}

var defaultCatalog = sync.OnceValue(func() *Catalog {
	catalog, err := New(catalogs)
	if err != nil {
		panic(err)
	}
	return catalog
})

// Default is initialized while constructing the handler, before serving.
func Default() *Catalog { return defaultCatalog() }

type Localizer struct {
	current, english *i18n.Localizer
	parameters       map[string][]string
}

func (c *Catalog) For(lang string) *Localizer {
	if lang != "ru" {
		lang = "en"
	}
	return &Localizer{current: i18n.NewLocalizer(c.bundle, lang), english: i18n.NewLocalizer(c.bundle, "en"), parameters: c.parameters}
}
func (l *Localizer) Render(text messages.Ref) string { return l.render(text, 0) }
func (l *Localizer) render(text messages.Ref, depth int) string {
	if messages.Empty(text) {
		return ""
	}
	if depth > 8 {
		return "The API could not complete the request."
	}
	d := messages.Describe(text)
	if d.IsLiteral {
		return d.Literal
	}
	data := make(map[string]any, len(d.Arguments))
	for _, arg := range d.Arguments {
		if _, duplicate := data[arg.Name()]; duplicate {
			return "The API could not complete the request."
		}
		v := arg.Value()
		if nested, ok := v.(messages.Ref); ok {
			v = l.render(nested, depth+1)
		}
		data[arg.Name()] = v
	}
	for _, name := range l.parameters[string(d.ID)] {
		if _, exists := data[name]; !exists {
			return "The API could not complete the request."
		}
	}
	config := &i18n.LocalizeConfig{MessageID: string(d.ID), TemplateData: data}
	if count, ok := data["Count"].(int64); ok {
		config.PluralCount = count
	}
	translated, err := l.current.Localize(config)
	if err == nil {
		return translated
	}
	translated, err = l.english.Localize(config)
	if err == nil {
		return translated
	}
	return "The API could not complete the request."
}

// Validate exposes only detached catalog diagnostics for checks, never inputs.
func Validate() error {
	_, err := New(catalogs)
	if err != nil {
		return fmt.Errorf("API localization: %w", err)
	}
	return nil
}
