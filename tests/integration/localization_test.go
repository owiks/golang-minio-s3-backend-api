package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
)

func TestLocalizationPreservesFileContentAndObjectLanguageNativeIntegration(t *testing.T) {
	c, s, b := fileFixture(t)
	key := "папка/русский-файл.txt"
	text := "Русский текст http.internal_error\nОшибка — это содержимое файла."
	_, err := s.PutObject(context.Background(), b, key, strings.NewReader(text), int64(len(text)), minio.PutObjectOptions{ContentType: "text/plain; charset=utf-8", ContentLanguage: "ja"})
	if err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"en", "ru"} {
		r, err := http.NewRequest("GET", c.Origin+c.Base+"/objects/content?"+url.Values{"bucket": {b}, "key": {key}}.Encode(), nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Accept-Language", lang)
		res, err := c.Http.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		var content struct{ Text, Revision string }
		if err := json.Unmarshal(body, &content); err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != 200 || res.Header.Get("Content-Language") != lang || content.Text != text || content.Revision == "" {
			t.Fatal(lang, res.StatusCode, string(body), res.Header)
		}
	}
	var link struct{ URL string }
	if err := json.Unmarshal(c.call("GET", "objects/url?"+url.Values{"bucket": {b}, "key": {key}, "mode": {"download"}}.Encode(), nil, 200), &link); err != nil {
		t.Fatal(err)
	}
	r, err := http.NewRequest("GET", link.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Accept-Language", "en")
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil || string(body) != text || res.Header.Get("Content-Language") != "ja" {
		t.Fatal("object bytes/language changed", string(body), res.Header, err)
	}
}
