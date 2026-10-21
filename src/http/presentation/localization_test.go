package presentation

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/http/requestctx"
	"minio-web/api/src/services/access"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/archives"
	"minio-web/api/src/services/archives/plan"
	"minio-web/api/src/services/buckets"
	"minio-web/api/src/services/iam"
	"minio-web/api/src/services/localization"
	"minio-web/api/src/services/messages"
	"minio-web/api/src/services/objects"
	"minio-web/api/src/services/transfers"
)

func languageContext(lang string) context.Context {
	return requestctx.WithLanguage(context.Background(), lang, true, localization.Default().For(lang))
}
func TestEmbeddedMessagesAndOpaqueUserData(t *testing.T) {
	opaque := "Русское имя http.internal_error"
	reason := messages.UploadSizeMismatch
	problem := apperror.Public(apperror.NewWithCause(400, "IncompleteUpload", reason, context.Canceled))
	policy := map[string]any{"Statement": opaque}
	values := map[string]any{
		"error":      problem,
		"access":     access.NavigationAccess{ActionCheck: &iam.ActionCheckIssue{Code: "AccountInfoUnavailable", Message: reason}},
		"details":    buckets.BucketDetails{Name: opaque, Policy: &buckets.DetailField{Value: policy}, Lifecycle: &buckets.DetailField{Value: buckets.LifecycleSnapshot{Warning: reason}}},
		"transfer":   transfers.TransferResult{Message: reason},
		"content":    objects.Content{Text: opaque, Reason: reason},
		"properties": objects.ObjectProperties{Metadata: map[string]string{"ru": opaque}, TagsError: reason},
	}
	for _, lang := range []string{"en", "ru"} {
		raw, err := json.Marshal(Render(languageContext(lang), values))
		if err != nil {
			t.Fatal(err)
		}
		var wire map[string]any
		if err := json.Unmarshal(raw, &wire); err != nil {
			t.Fatal(err)
		}
		want := localization.Default().For(lang).Render(reason)
		for _, item := range []string{"error", "transfer"} {
			if wire[item].(map[string]any)["message"] != want {
				t.Fatal(item, string(raw))
			}
		}
		if wire["content"].(map[string]any)["text"] != opaque || wire["content"].(map[string]any)["reason"] != want {
			t.Fatal(string(raw))
		}
		if got := wire["details"].(map[string]any)["policy"].(map[string]any)["value"]; !reflect.DeepEqual(got, policy) {
			t.Fatal("policy was translated", got)
		}
		if !strings.Contains(string(raw), opaque) || strings.Contains(string(raw), string(reason)) {
			t.Fatal("opaque data or descriptor changed", string(raw))
		}
	}
	if problem.Status != 400 || problem.Code != "IncompleteUpload" || messages.Identity(problem.Message) != messages.Identity(reason) {
		t.Fatal("source error mutated")
	}
}
func TestSavedArchiveAndRevisionDoNotDependOnLanguage(t *testing.T) {
	plan := archiveplan.New(archiveplan.Config{SessionID: "owner", Bucket: "бакет", Created: time.Now(), Items: []archiveplan.ArchiveItem{{Key: "файл", State: "error", Message: messages.UploadSizeMismatch}}, Issues: map[string]messages.Ref{"папка": messages.UploadInterrupted}})
	before := plan.Snapshot("ticket")
	properties := objects.ObjectProperties{Headers: map[string]string{"Content-Language": "ru"}, Tags: map[string]string{"владелец": "я"}, TagsError: messages.UploadInterrupted}
	info := minio.ObjectInfo{ETag: "etag", Size: 5}
	revision := objects.PropertiesRevision(info, properties)
	var texts []string
	for _, lang := range []string{"ru", "en", "ru"} {
		raw, err := json.Marshal(Render(languageContext(lang), plan.Snapshot("ticket")))
		if err != nil {
			t.Fatal(err)
		}
		texts = append(texts, string(raw))
		if _, err := json.Marshal(Render(languageContext(lang), archives.Manifest{Bucket: "бакет", Files: before.Items, Issues: before.Issues})); err != nil {
			t.Fatal(err)
		}
		if objects.PropertiesRevision(info, properties) != revision {
			t.Fatal("revision changed")
		}
	}
	if !reflect.DeepEqual(before, plan.Snapshot("ticket")) || texts[0] != texts[2] || texts[0] == texts[1] {
		t.Fatal("localized snapshot changed state", texts)
	}
	properties.TagsError = messages.UploadSizeMismatch
	if objects.PropertiesRevision(info, properties) != revision {
		t.Fatal("display message participates in revision")
	}
	properties.Headers["Content-Language"] = "en"
	if objects.PropertiesRevision(info, properties) == revision {
		t.Fatal("real metadata no longer participates in revision")
	}
}
func TestLanguageHeadersPreserveVary(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  []string
	}{{"Origin", []string{"Origin", "Accept-Language"}}, {"*", []string{"*"}}, {"origin, accept-language", []string{"origin, accept-language"}}} {
		h := http.Header{}
		h.Set("Vary", tc.value)
		LanguageHeaders(languageContext("ru"), h)
		LanguageHeaders(languageContext("ru"), h)
		if !reflect.DeepEqual(h.Values("Vary"), tc.want) || h.Get("Content-Language") != "ru" {
			t.Fatal(h)
		}
	}
}
