package integration

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/localization"
	objectwrite "github.com/owiks/golang-minio-s3-backend-api/src/services/objects/write"
)

func localizedRequest(t *testing.T, c *apiClient, method, path string, body any, lang string, want int) ([]byte, http.Header) {
	t.Helper()
	var input io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		input = bytes.NewReader(raw)
	}
	r, err := http.NewRequest(method, c.Origin+c.Base+"/"+path, input)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Accept-Language", lang)
	r.Header.Set("Origin", c.Origin)
	r.Header.Set("X-CSRF-Token", c.CSRF)
	r.Header.Set("Content-Type", "application/json")
	res, err := c.Http.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != want {
		t.Fatalf("%s %s (%s): %d want %d: %s", method, path, lang, res.StatusCode, want, raw)
	}
	assertResponseContract(t, method, r.URL.Path, res.StatusCode, res.Header.Get("Content-Type"), raw)
	return raw, res.Header.Clone()
}
func decodeLocalized(t *testing.T, raw []byte, out any) {
	t.Helper()
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatal(err)
	}
}

func TestLocalizationAuditRevisionsAndFileOperationsNativeIntegration(t *testing.T) {
	c, s, b := fileFixture(t)
	ctx := context.Background()
	original := "Данные файла http.internal_error {{.Name}}\n"
	for _, lang := range []string{"en", "ru"} {
		source := "source-" + lang + ".txt"
		dest := "copy-" + lang + ".txt"
		moved := "moved-" + lang + ".txt"
		_, err := s.PutObject(ctx, b, source, strings.NewReader(original), int64(len(original)), minio.PutObjectOptions{ContentType: "text/plain", ContentLanguage: "ja", UserMetadata: map[string]string{"source": "http.internal_error"}, UserTags: map[string]string{"scope": "audit"}})
		if err != nil {
			t.Fatal(err)
		}
		var baseline struct{ Revision string }
		for _, requested := range []string{"en", "ru", "en"} {
			raw, _ := localizedRequest(t, c, "GET", "objects/properties?"+url.Values{"bucket": {b}, "key": {source}}.Encode(), nil, requested, 200)
			var p struct {
				Revision                string
				Headers, Metadata, Tags map[string]string
			}
			decodeLocalized(t, raw, &p)
			if baseline.Revision != "" && baseline.Revision != p.Revision {
				t.Fatal("locale changed property revision")
			}
			baseline.Revision = p.Revision
			if p.Headers["Content-Language"] != "ja" || p.Metadata["source"] != "http.internal_error" || p.Tags["scope"] != "audit" {
				t.Fatal("locale changed stored properties", p)
			}
		}
		var state objectwrite.FileState
		raw, _ := localizedRequest(t, c, "GET", "objects/prepare?"+url.Values{"bucket": {b}, "key": {source}}.Encode(), nil, lang, 200)
		decodeLocalized(t, raw, &state)
		body := map[string]any{"sourceBucket": b, "sourceKey": source, "sourceRevision": state.Revision, "destinationBucket": b, "destinationKey": dest, "destinationRevision": "absent", "move": false}
		raw, _ = localizedRequest(t, c, "POST", "objects/transfer", body, lang, 200)
		var copied struct {
			Copied, Moved bool
			Code, Message string
		}
		decodeLocalized(t, raw, &copied)
		if !copied.Copied || copied.Moved || copied.Code != "" || copied.Message == "" {
			t.Fatal(copied)
		}
		var conflictCode string
		for _, requested := range []string{"en", "ru"} {
			raw, _ = localizedRequest(t, c, "POST", "objects/transfer", body, requested, 409)
			var conflict struct {
				Error struct{ Code, Message string }
			}
			decodeLocalized(t, raw, &conflict)
			if conflictCode != "" && conflictCode != conflict.Error.Code {
				t.Fatal("locale changed conflict code")
			}
			conflictCode = conflict.Error.Code
			if conflictCode == "" || conflict.Error.Message == "" {
				t.Fatal(conflict)
			}
		}
		body["destinationKey"] = moved
		body["move"] = true
		raw, _ = localizedRequest(t, c, "POST", "objects/transfer", body, lang, 200)
		decodeLocalized(t, raw, &copied)
		if !copied.Copied || !copied.Moved || copied.Code != "" {
			t.Fatal(copied)
		}
		if _, err := s.StatObject(ctx, b, source, minio.StatObjectOptions{}); minio.ToErrorResponse(err).Code != "NoSuchKey" {
			t.Fatal("source remains", err)
		}
		for _, key := range []string{dest, moved} {
			o, err := s.GetObject(ctx, b, key, minio.GetObjectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(o)
			o.Close()
			if err != nil || string(data) != original {
				t.Fatal("locale changed copied bytes", err)
			}
			info, err := s.StatObject(ctx, b, key, minio.StatObjectOptions{})
			if err != nil || info.Metadata.Get("Content-Language") != "ja" || info.UserMetadata["Source"] != "http.internal_error" {
				t.Fatal("locale changed copied metadata", info, err)
			}
		}
	}
	var revision string
	for _, lang := range []string{"ru", "en", "ru"} {
		raw, _ := localizedRequest(t, c, "GET", "admin/server-config?subsystem=api", nil, lang, 200)
		var view struct{ Revision string }
		decodeLocalized(t, raw, &view)
		if view.Revision == "" || revision != "" && view.Revision != revision {
			t.Fatal("locale changed server revision", view)
		}
		revision = view.Revision
	}
}
func TestLocalizationAuditZIPHeadersManifestAndStateNativeIntegration(t *testing.T) {
	for _, mode := range []string{"auto", "en", "ru"} {
		t.Run(mode, func(t *testing.T) {
			c, s, b := fileFixture(t, func(cfg *config.Config) { cfg.Language = mode })
			key := "http.internal_error.txt"
			text := "Русский текст файла {{.Name}}"
			_, err := s.PutObject(context.Background(), b, key, strings.NewReader(text), int64(len(text)), minio.PutObjectOptions{ContentType: "text/plain", ContentLanguage: "ja"})
			if err != nil {
				t.Fatal(err)
			}
			for _, requested := range []string{"en", "ru"} {
				// A key missing before preparation correctly rejects the request.
				// Remove it after the plan is accepted to exercise a partial ZIP.
				_, err := s.PutObject(context.Background(), b, "missing.txt", strings.NewReader("temporary"), 9, minio.PutObjectOptions{})
				if err != nil {
					t.Fatal(err)
				}
				lang := mode
				if mode == "auto" {
					lang = requested
				}
				raw, _ := localizedRequest(t, c, "POST", "objects/archive", map[string]any{"bucket": b, "keys": []string{key, "missing.txt"}}, requested, 200)
				var prepared struct{ Ticket, Name string }
				decodeLocalized(t, raw, &prepared)
				if err := s.RemoveObject(context.Background(), b, "missing.txt", minio.RemoveObjectOptions{}); err != nil {
					t.Fatal(err)
				}
				localizedRequest(t, c, "PUT", "objects/archive/"+prepared.Ticket, map[string]any{"allowPartial": true, "name": prepared.Name}, requested, 200)
				raw, headers := localizedRequest(t, c, "GET", "objects/archive/"+prepared.Ticket, nil, requested, 200)
				if headers.Get("Content-Language") != lang || strings.Contains(headers.Get("Vary"), "Accept-Language") != (mode == "auto") {
					t.Fatal("generated ZIP omitted language headers", headers)
				}
				zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
				if err != nil {
					t.Fatal(err)
				}
				haveData, haveManifest := false, false
				for _, file := range zr.File {
					f, err := file.Open()
					if err != nil {
						t.Fatal(err)
					}
					data, err := io.ReadAll(f)
					f.Close()
					if err != nil {
						t.Fatal(err)
					}
					if file.Name == key {
						haveData = string(data) == text
						continue
					}
					if strings.HasPrefix(file.Name, ".minio-web-results-") {
						var manifest struct {
							Files []struct{ Key, State, Message string }
						}
						decodeLocalized(t, data, &manifest)
						for _, item := range manifest.Files {
							if item.Key == "missing.txt" {
								want := localization.Default().For(lang).Render(apperror.Public(minio.ErrorResponse{Code: "NoSuchKey"}).Message)
								haveManifest = item.State == "error" && item.Message == want
							}
						}
					}
				}
				if !haveData || !haveManifest {
					t.Fatal("ZIP changed file bytes or failed to translate manifest", haveData, haveManifest)
				}
				type item struct {
					Key, Path, State string
					Size, Sent       int64
				}
				type snapshot struct {
					Ticket, Name, Base, RelativeBase string
					Count                            int
					Size, Sent                       int64
					Started, Finished, Cancelled     bool
					Items                            []item
					ExpiresAt                        time.Time
				}
				var before snapshot
				for _, request := range []string{"ru", "en", "ru"} {
					raw, _ := localizedRequest(t, c, "GET", "objects/archive-status/"+prepared.Ticket, nil, request, 200)
					var current snapshot
					decodeLocalized(t, raw, &current)
					if before.Ticket != "" && !reflect.DeepEqual(current, before) {
						t.Fatal("locale changed saved archive state")
					}
					before = current
				}
				if !before.Finished || before.Cancelled {
					t.Fatal(before)
				}
			}
		})
	}
}
