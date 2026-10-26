package controllers

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/middleware"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/requestctx"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/localization"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

func TestArchiveDownloadLanguage(t *testing.T) {
	for _, tc := range []struct{ mode, query, header, want string }{
		{"auto", "?lang=ru", "en", "ru"},
		{"auto", "?lang=en", "ru", "en"},
		{"auto", "", "ru-RU, en;q=0.5", "ru"},
		{"auto", "", "de", "en"},
		{"en", "?lang=ru", "ru", "en"},
		{"ru", "?lang=en", "en", "ru"},
	} {
		t.Run(tc.mode+"/"+tc.query+"/"+tc.header, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/"+tc.query, nil)
			r.Header.Set("Accept-Language", tc.header)
			r = middleware.LocalizeRequest(r, tc.mode, nil)
			before := requestctx.Language(r.Context())
			got, err := archiveDownloadLanguage(r)
			if err != nil || requestctx.Language(got.Context()) != tc.want || requestctx.AutomaticLanguage(got.Context()) != (tc.mode == "auto") {
				t.Fatal("incorrect archive presentation language", err, got)
			}
			if requestctx.Language(r.Context()) != before {
				t.Fatal("modified the original request language")
			}
		})
	}
}

func TestArchiveLanguageRejectsInvalidValuesBeforeStartingDownload(t *testing.T) {
	for _, mode := range []string{"auto", "en", "ru"} {
		for _, query := range []string{"?lang=", "?lang=ru-RU", "?lang=RU", "?lang=PRIVATE_LANGUAGE", "?lang=ru&lang=en", "?lang=ru&lang=ru", "?lang=%zz", "?lang=ru;en"} {
			r := middleware.LocalizeRequest(httptest.NewRequest("GET", "/"+query, nil), mode, nil)
			w := httptest.NewRecorder()
			// Nil dependencies ensure validation finishes before touching a plan or client.
			err := HandleDownloadArchive(nil, w, r, nil, "unused-ticket")
			if err == nil || apperror.Public(err).Status != 400 || apperror.Public(err).Code != "InvalidLanguage" || w.Body.Len() != 0 || len(w.Header()) != 0 {
				t.Fatal("invalid language reached archive execution", mode, query, err, w.Body, w.Header())
			}
		}
	}
}

func TestArchiveLanguageUsesTheOriginalRequestCatalog(t *testing.T) {
	catalog, err := localization.New(fstest.MapFS{
		"locales/active.en.json": {Data: fmt.Appendf(nil, `[{"id":%q,"other":"custom English"}]`, messages.InternalError)},
		"locales/active.ru.json": {Data: fmt.Appendf(nil, `[{"id":%q,"other":"custom Russian"}]`, messages.InternalError)},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/?lang=ru", nil)
	r = r.WithContext(requestctx.WithLanguage(context.Background(), "en", true, catalog.For("en")))
	got, err := archiveDownloadLanguage(r)
	if err != nil || requestctx.Localizer(got.Context()).Render(messages.InternalError) != "custom Russian" || requestctx.Localizer(r.Context()).Render(messages.InternalError) != "custom English" {
		t.Fatal("archive used a different catalog or mutated its existing localizer", err)
	}
}
