package archives

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/minio/minio-go/v7"

	archiveplan "github.com/owiks/golang-minio-s3-backend-api/src/services/archives/plan"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/localization"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

func testManifest(manifest Manifest) any {
	return testManifestLanguage(manifest, "en")
}
func testManifestLanguage(manifest Manifest, lang string) any {
	localizer := localization.Default().For(lang)
	type item struct {
		archiveplan.ArchiveItem
		Message string `json:"message,omitempty"`
	}
	files := make([]item, len(manifest.Files))
	for i, file := range manifest.Files {
		files[i] = item{file, localizer.Render(file.Message)}
	}
	issues := make(map[string]string, len(manifest.Issues))
	for key, value := range manifest.Issues {
		issues[key] = localizer.Render(value)
	}
	return struct {
		Bucket string            `json:"bucket"`
		Files  []item            `json:"files"`
		Issues map[string]string `json:"issues"`
	}{manifest.Bucket, files, issues}
}

func TestManifestIsRenderedAtDeliveryWithoutChangingPlan(t *testing.T) {
	for _, lang := range []string{"en", "ru"} {
		source := &downloadSource{read: func(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error) {
			return nil, minio.ErrorResponse{Code: "NoSuchKey"}
		}}
		download, plan, _ := startTestDownload(t, context.Background(), source, []archiveplan.ArchiveItem{pendingArchiveItem()})
		var data bytes.Buffer
		err := download.StreamTo(&data, func(m Manifest) any { return testManifestLanguage(m, lang) })
		if err != nil {
			t.Fatal(err)
		}
		z, err := zip.NewReader(bytes.NewReader(data.Bytes()), int64(data.Len()))
		if err != nil {
			t.Fatal(err)
		}
		if len(z.File) != 1 {
			t.Fatal("missing manifest", len(z.File))
		}
		f, err := z.File[0].Open()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		snapshot := plan.Snapshot("ticket-12345678")
		var body struct {
			Files []struct{ Key, State, Message string }
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		if len(body.Files) != 1 || body.Files[0].State != "error" || body.Files[0].Key != "file" || body.Files[0].Message != localization.Default().For(lang).Render(snapshot.Items[0].Message) {
			t.Fatal(string(raw))
		}
		identity := messages.Identity(snapshot.Items[0].Message)
		_ = testManifestLanguage(Manifest{Files: snapshot.Items}, "en")
		_ = testManifestLanguage(Manifest{Files: snapshot.Items}, "ru")
		if identity != messages.Identity(plan.Snapshot("ticket-12345678").Items[0].Message) {
			t.Fatal("plan stored rendered text")
		}
	}
}
func TestMissingManifestProjectionDoesNotStartStream(t *testing.T) {
	source := &downloadSource{read: func(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error) {
		t.Error("missing renderer must not read files")
		return nil, context.Canceled
	}}
	download, _, _ := startTestDownload(t, context.Background(), source, []archiveplan.ArchiveItem{pendingArchiveItem()})
	var output bytes.Buffer
	if err := download.StreamTo(&output, nil); err == nil || output.Len() != 0 || source.calls.Load() != 0 {
		t.Fatal("missing projection started download", err)
	}
}
