// Package messages describes user-facing text without choosing a language.
package messages

import (
	"encoding/json"
	"errors"
	"slices"
	"sort"
)

// Ref is sealed: only immutable keys, literals and parameterized messages can
// enter a result. SDK errors and credentials cannot become template data.
type Ref interface{ describe() Description }

type ID string

func (id ID) describe() Description { return Description{ID: id} }
func (id ID) With(args ...Argument) Ref {
	return value{id: id, args: slices.Clone(args)}
}

type Argument struct {
	name string
	data any
}

func String(name, text string) Argument   { return Argument{name: name, data: text} }
func Int(name string, n int64) Argument   { return Argument{name: name, data: n} }
func Text(name string, text Ref) Argument { return Argument{name: name, data: text} }
func (a Argument) Name() string           { return a.name }
func (a Argument) Value() any             { return a.data }

type Description struct {
	ID        ID
	Literal   string
	IsLiteral bool
	Arguments []Argument
}

type value struct {
	id   ID
	args []Argument
}

func (v value) describe() Description {
	return Description{ID: v.id, Arguments: slices.Clone(v.args)}
}

type literal string

func (v literal) describe() Description {
	return Description{Literal: string(v), IsLiteral: true}
}

// Literal is only for opaque user data, such as names of policies/principals.
func Literal(text string) Ref {
	if text == "" {
		return nil
	}
	return literal(text)
}
func Describe(text Ref) Description {
	if text == nil {
		return Description{}
	}
	return text.describe()
}
func Empty(text Ref) bool {
	d := Describe(text)
	return d.ID == "" && d.Literal == ""
}

func Literals(values []string) []Ref {
	if values == nil {
		return nil
	}
	result := make([]Ref, len(values))
	for i, value := range values {
		result[i] = Literal(value)
	}
	return result
}

const Combined ID = "common.combined"

func Join(first, second Ref) Ref {
	if Empty(first) {
		return second
	}
	if Empty(second) {
		return first
	}
	return Combined.With(Text("First", first), Text("Second", second))
}

// Identity is for deduplication and revisions, never display or JSON output.
func Identity(text Ref) string {
	var describe func(Ref, int) any
	describe = func(r Ref, depth int) any {
		if depth > 8 {
			return nil
		}
		d := Describe(r)
		if d.IsLiteral {
			return []any{"literal", d.Literal}
		}
		args := map[string]any{}
		for _, arg := range d.Arguments {
			v := arg.Value()
			if nested, ok := v.(Ref); ok {
				v = describe(nested, depth+1)
			}
			args[arg.Name()] = v
		}
		return []any{string(d.ID), args}
	}
	data, _ := json.Marshal(describe(text, 0))
	return string(data)
}

func SortedUnique(values []Ref) []Ref {
	byID := make(map[string]Ref, len(values))
	for _, value := range values {
		byID[Identity(value)] = value
	}
	keys := make([]string, 0, len(byID))
	for key := range byID {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]Ref, 0, len(keys))
	for _, key := range keys {
		result = append(result, byID[key])
	}
	return result
}

// Descriptors must pass through an explicit HTTP projection. Never serialize
// an internal ID when a newly added response forgot its projection.
var ErrUnrendered = errors.New("message requires HTTP localization")

func (ID) MarshalJSON() ([]byte, error)      { return nil, ErrUnrendered }
func (value) MarshalJSON() ([]byte, error)   { return nil, ErrUnrendered }
func (literal) MarshalJSON() ([]byte, error) { return nil, ErrUnrendered }
