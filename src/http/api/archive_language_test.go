package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/doc"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/archives/plan"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/localization"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/tests/testsession"
)

func TestArchiveDownloadQueryLocalizesManifestOnly(t *testing.T) {
	for _, tc := range []struct{ mode, query, header, want string }{
		{"auto", "?lang=ru", "en", "ru"},
		{"auto", "?lang=en", "ru", "en"},
		{"auto", "", "ru", "ru"},
		{"en", "?lang=ru", "ru", "en"},
		{"ru", "?lang=en", "en", "ru"},
	} {
		t.Run(tc.mode+"/"+tc.query+"/"+tc.header, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" && r.Method != "HEAD" {
					t.Error("archive attempted a native write", r.Method)
				}
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, `<Error><Code>NoSuchKey</Code><Message>PRIVATE-UPSTREAM</Message></Error>`)
			}))
			defer upstream.Close()
			cfg := config.Default()
			cfg.Language = tc.mode
			app := New(cfg)
			defer app.Close()
			s := testsession.New(t, upstream.URL)
			if err := app.StoreSession(s); err != nil {
				t.Fatal(err)
			}
			cached := archiveplan.New(archiveplan.Config{SessionID: s.SessionID(), Bucket: "data", Name: "files.zip", Created: time.Now(), AllowPartial: true,
				Items:  []archiveplan.ArchiveItem{{Key: "пользовательский-путь.txt", Path: "пользовательский-путь.txt", State: "pending"}},
				Issues: map[string]messages.Ref{"user-created-issue": messages.UploadSizeMismatch}})
			ticket, err := app.Archives.Put(context.Background(), cached)
			if err != nil {
				t.Fatal(err)
			}
			path := "/api/sessions/" + s.SessionID() + "/objects/archive/" + ticket
			r := httptest.NewRequest("GET", "http://api.test"+path+tc.query, nil)
			r.Header.Set("Accept-Language", tc.header)
			r.AddCookie(&http.Cookie{Name: "minio_session_" + s.SessionID(), Value: s.CookieSnapshot().Token})
			w := httptest.NewRecorder()
			app.ServeHTTP(w, r)
			if w.Code != 200 || w.Header().Get("Content-Type") != "application/zip" || w.Header().Get("Content-Language") != tc.want || strings.Contains(w.Header().Get("Vary"), "Accept-Language") != (tc.mode == "auto") {
				t.Fatal("incorrect archive response", w.Code, w.Header(), w.Body.String())
			}
			z, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
			if err != nil || len(z.File) != 1 {
				t.Fatal("missing archive manifest", err)
			}
			file, err := z.File[0].Open()
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(file)
			_ = file.Close()
			if err != nil {
				t.Fatal(err)
			}
			var manifest struct {
				Bucket string
				Files  []struct{ Key, State, Message string }
				Issues map[string]string
			}
			if err := json.Unmarshal(raw, &manifest); err != nil {
				t.Fatal(err)
			}
			snapshot := cached.Snapshot(ticket)
			localizer := localization.Default().For(tc.want)
			if len(manifest.Files) != 1 || manifest.Bucket != "data" || manifest.Files[0].Key != "пользовательский-путь.txt" || manifest.Files[0].State != "error" || manifest.Files[0].Message != localizer.Render(snapshot.Items[0].Message) || manifest.Issues["user-created-issue"] != localizer.Render(messages.UploadSizeMismatch) || strings.Contains(string(raw), "PRIVATE-UPSTREAM") || strings.Contains(string(raw), string(messages.UploadSizeMismatch)) {
				t.Fatal("manifest changed user data or exposed untranslated/internal text", string(raw))
			}
			if !snapshot.Started || !snapshot.Finished || snapshot.Cancelled || messages.Identity(snapshot.Issues["user-created-issue"]) != messages.Identity(messages.UploadSizeMismatch) {
				t.Fatal("language affected archive lifecycle or saved message descriptions", snapshot)
			}
		})
	}
}

func TestArchiveLanguageValidationDoesNotBypassAuthenticationOrConsumePlan(t *testing.T) {
	app := New(config.Default())
	defer app.Close()
	s := testsession.New(t, "http://fixture.invalid")
	if err := app.StoreSession(s); err != nil {
		t.Fatal(err)
	}
	cached := archiveplan.New(archiveplan.Config{SessionID: s.SessionID(), Bucket: "data", Name: "files.zip", Created: time.Now(), Items: []archiveplan.ArchiveItem{{Key: "file.txt", State: "pending"}}})
	ticket, err := app.Archives.Put(context.Background(), cached)
	if err != nil {
		t.Fatal(err)
	}
	before := cached.Snapshot(ticket)
	for _, tc := range []struct {
		query, token, code string
		status             int
	}{
		{"?lang=ru", "invalid-bearer", "InvalidToken", 401},
		{"?lang=PRIVATE_LANGUAGE", "invalid-bearer", "InvalidToken", 401},
		{"?lang=PRIVATE_LANGUAGE", "", "InvalidLanguage", 400},
		{"?lang=ru&lang=en", "", "InvalidLanguage", 400},
	} {
		r := httptest.NewRequest("GET", "http://api.test/api/sessions/"+s.SessionID()+"/objects/archive/"+ticket+tc.query, nil)
		r.Header.Set("Accept-Language", "en")
		r.AddCookie(&http.Cookie{Name: "minio_session_" + s.SessionID(), Value: s.CookieSnapshot().Token})
		if tc.token != "" {
			r.Header.Set("Authorization", "Bearer "+tc.token)
		}
		w := httptest.NewRecorder()
		app.ServeHTTP(w, r)
		var result struct {
			Error struct{ Code, Message string }
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if w.Code != tc.status || result.Error.Code != tc.code || result.Error.Message == "" || strings.Contains(w.Body.String(), "PRIVATE_LANGUAGE") || strings.Contains(w.Body.String(), string(messages.OperationInvalid)) || w.Header().Get("Content-Language") != "en" {
			t.Fatal("incorrect language validation/authentication order", tc, w.Code, w.Body, w.Header())
		}
		if !reflect.DeepEqual(before, cached.Snapshot(ticket)) {
			t.Fatal("rejected download consumed or modified the plan")
		}
	}
}

func TestArchiveDownloadLanguageParameterIsSpecificAndDocumented(t *testing.T) {
	document := doc.Build(Catalog())
	path := "/api/sessions/{sessionID}/objects/archive/{ticket}"
	parameter := inputParameter(t, document.Paths.Value(path).Get, "lang")
	if parameter.In != "query" || parameter.Required || parameter.Schema == nil || !parameter.Schema.Value.Type.Is("string") || !reflect.DeepEqual(parameter.Schema.Value.Enum, []any{"en", "ru"}) || !strings.Contains(parameter.Description, "API_LANGUAGE=auto") {
		t.Fatal("incorrect ZIP language contract", parameter)
	}
	for _, method := range []string{"PUT", "DELETE"} {
		for _, ref := range document.Paths.Value(path).GetOperation(method).Parameters {
			if ref.Value.Name == "lang" {
				t.Fatal("language query leaked into another action", method)
			}
		}
	}
}
