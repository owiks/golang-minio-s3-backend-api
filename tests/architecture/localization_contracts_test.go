package architecture

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/api"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/presentation"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/requestctx"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/archives"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/localization"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

// Reflection is confined to the test fixture: exercise every declared response
// containing descriptors, including nested slices/maps. Production projections
// remain explicit and never walk arbitrary user data.
func modelWithMessages(typ reflect.Type, depth int) (reflect.Value, int) {
	out := reflect.New(typ).Elem()
	if depth > 12 {
		return out, 0
	}
	if typ == reflect.TypeFor[messages.Ref]() {
		out.Set(reflect.ValueOf(messages.UploadSizeMismatch))
		return out, 1
	}
	count := 0
	switch typ.Kind() {
	case reflect.Struct:
		for i := range typ.NumField() {
			f := typ.Field(i)
			if !f.IsExported() || f.Tag.Get("json") == "-" {
				continue
			}
			value, n := modelWithMessages(f.Type, depth+1)
			if n > 0 {
				out.Field(i).Set(value)
				count += n
			}
		}
	case reflect.Pointer:
		value, n := modelWithMessages(typ.Elem(), depth+1)
		if n > 0 {
			out.Set(reflect.New(typ.Elem()))
			out.Elem().Set(value)
			count = n
		}
	case reflect.Slice:
		value, n := modelWithMessages(typ.Elem(), depth+1)
		if n > 0 {
			out = reflect.MakeSlice(typ, 1, 1)
			out.Index(0).Set(value)
			count = n
		}
	case reflect.Map:
		if typ.Key().Kind() == reflect.String {
			value, n := modelWithMessages(typ.Elem(), depth+1)
			if n > 0 {
				out = reflect.MakeMap(typ)
				key := reflect.New(typ.Key()).Elem()
				key.SetString("opaque-user-key")
				out.SetMapIndex(key, value)
				count = n
			}
		}
	}
	return out, count
}
func TestEveryLocalizedResponseHasAnExplicitProjection(t *testing.T) {
	checked := map[reflect.Type]bool{}
	var check func(any)
	check = func(model any) {
		if model == nil {
			return
		}
		typ := reflect.TypeOf(model)
		if checked[typ] {
			return
		}
		checked[typ] = true
		if strings.HasPrefix(typ.Name(), "StreamEvent[") {
			// Services emit the envelope; WriteNDJSON projects Result and Error
			// separately. Check those declared models instead of skipping the stream.
			for _, name := range []string{"Result", "Error"} {
				field, exists := typ.FieldByName(name)
				if !exists || field.Type.Kind() != reflect.Pointer {
					t.Fatal("invalid stream contract", typ, name)
				}
				check(reflect.Zero(field.Type.Elem()).Interface())
			}
			return
		}
		sample, n := modelWithMessages(typ, 0)
		if n == 0 {
			return
		}
		for _, lang := range []string{"en", "ru"} {
			localizer := localization.Default().For(lang)
			ctx := requestctx.WithLanguage(context.Background(), lang, true, localizer)
			raw, err := json.Marshal(presentation.Render(ctx, sample.Interface()))
			if err != nil {
				t.Errorf("%s/%s has an unprojected descriptor: %v", typ, lang, err)
				continue
			}
			if strings.Contains(string(raw), string(messages.UploadSizeMismatch)) {
				t.Error("internal ID in JSON", typ)
			}
			want, _ := json.Marshal(localizer.Render(messages.UploadSizeMismatch))
			if got := strings.Count(string(raw), string(want)); got != n {
				t.Errorf("%s/%s lost messages: got %d want %d: %s", typ, lang, got, n, raw)
			}
		}
	}
	for _, route := range api.Catalog() {
		for _, op := range route.Operations {
			check(op.Response)
			for _, response := range op.Responses {
				check(response.Model)
				for _, variant := range response.Variants {
					check(variant)
				}
			}
		}
	}
	check(archives.Manifest{})
}
