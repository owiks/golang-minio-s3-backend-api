package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"minio-web/api/src/http/presentation"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/iam"
	objectssvc "minio-web/api/src/services/objects"
	resultdto "minio-web/api/src/services/result"
	storagesvc "minio-web/api/src/services/storage"
)

func TestPropertyPatchValidationAndMerge(t *testing.T) {
	if e := objectssvc.ValidatePropertyPatch(map[string]string{"Content-Type": "bad"}, nil, nil, "", ""); e == nil {
		t.Fatal("invalid MIME accepted")
	}
	if e := objectssvc.ValidatePropertyPatch(map[string]string{"Cache-Control": "one\r\nInjected: yes"}, nil, nil, "", ""); e == nil {
		t.Fatal("header injection accepted")
	}
	if e := objectssvc.ValidatePropertyPatch(nil, map[string]string{"x-amz-storage-class": "GLACIER"}, nil, "", ""); e == nil {
		t.Fatal("reserved metadata accepted")
	}
	if e := objectssvc.ValidatePropertyPatch(nil, map[string]string{"key": strings.Repeat("x", 2049)}, nil, "", ""); e == nil {
		t.Fatal("oversized metadata")
	}
	merged := objectssvc.MergeProperties(map[string]string{"keep": "yes", "old": "yes"}, map[string]string{"old": "new"}, "merge")
	if merged["keep"] != "yes" || merged["old"] != "new" {
		t.Fatal(merged)
	}
	if _, exists := objectssvc.MergeProperties(merged, map[string]string{"old": ""}, "remove")["old"]; exists {
		t.Fatal("remove failed")
	}
}
func TestChecksumTypesDoNotTreatETagOrCompositeAsFull(t *testing.T) {
	rows := objectssvc.AvailableChecksums(minio.ObjectInfo{ETag: strings.Repeat("a", 32), ChecksumSHA256: "encoded-3", ChecksumMode: "FULL_OBJECT"})
	if len(rows) != 1 || rows[0].Type != "COMPOSITE" {
		t.Fatal(rows)
	}
	if rows := objectssvc.AvailableChecksums(minio.ObjectInfo{ETag: strings.Repeat("a", 32)}); len(rows) != 0 {
		t.Fatal("ETag exposed as checksum", rows)
	}
}
func TestObjectPropertiesIntegration(t *testing.T) {
	c, client, bucket := fileFixture(t)
	ctx := context.Background()
	key := "opaque/Свойства + # %.txt"
	content := "unchanged body"
	_, err := client.PutObject(ctx, bucket, key, strings.NewReader(content), int64(len(content)), minio.PutObjectOptions{ContentType: "text/plain", CacheControl: "private", ContentDisposition: "attachment", ContentLanguage: "ru", UserMetadata: map[string]string{"keep": "original"}, UserTags: map[string]string{"owner": "team"}})
	if err != nil {
		t.Fatal(err)
	}
	route := "objects/properties?" + url.Values{"bucket": {bucket}, "key": {key}}.Encode()
	var before presentation.ObjectProperties
	_ = json.Unmarshal(c.call("GET", route, nil, 200), &before)
	c.call("PUT", route, map[string]any{"revision": before.Revision, "headers": map[string]string{"Content-Type": "application/json", "Cache-Control": "public, max-age=60"}, "metadata": map[string]string{"added": "yes", "content-type": "custom only"}, "metadataMode": "merge", "tags": map[string]string{"env": "test"}, "tagsMode": "merge"}, 200)
	object, err := client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(object)
	_ = object.Close()
	if err != nil || string(data) != content {
		t.Fatal("body changed", string(data), err)
	}
	info, err := client.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil || info.ContentType != "application/json" || info.UserMetadata["Keep"] != "original" && info.UserMetadata["keep"] != "original" || info.Metadata.Get("Content-Language") != "ru" || info.Metadata.Get("Content-Disposition") != "attachment" {
		t.Fatalf("properties lost: %+v %v", info, err)
	}
	var after presentation.ObjectProperties
	_ = json.Unmarshal(c.call("GET", route, nil, 200), &after)
	if after.Tags["owner"] != "team" || after.Tags["env"] != "test" || after.Metadata["keep"] != "original" || after.Metadata["content-type"] != "custom only" {
		t.Fatal(after)
	}
	c.call("PUT", route, map[string]any{"revision": before.Revision, "tags": map[string]string{"bad": "stale"}}, 409)
	c.call("PUT", route, map[string]any{"revision": after.Revision, "tags": map[string]string{"owner": ""}, "tagsMode": "remove"}, 200)
	var tagged presentation.ObjectProperties
	_ = json.Unmarshal(c.call("GET", route, nil, 200), &tagged)
	if _, ok := tagged.Tags["owner"]; ok || tagged.Tags["env"] != "test" {
		t.Fatal(tagged)
	}
	_, err = client.PutObject(ctx, bucket, key, strings.NewReader("new body"), 8, minio.PutObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c.call("PUT", route, map[string]any{"revision": tagged.Revision, "headers": map[string]string{"Content-Type": "text/plain"}}, 409)
}
func TestObjectChecksumIntegration(t *testing.T) {
	c, client, bucket := fileFixture(t)
	ctx := context.Background()
	key := "checksum.bin"
	data := bytes.Repeat([]byte("integrity"), 128*1024)
	sum := sha256.Sum256(data)
	expected := hex.EncodeToString(sum[:])
	_, err := client.PutObject(ctx, bucket, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{AutoChecksum: minio.ChecksumSHA256, DisableMultipart: true})
	if err != nil {
		t.Fatal(err)
	}
	route := "objects/checksum?" + url.Values{"bucket": {bucket}, "key": {key}}.Encode()
	var summary presentation.ChecksumInfo
	if err := json.Unmarshal(c.call("GET", route, nil, 200), &summary); err != nil || summary.Size != int64(len(data)) || summary.ETag == "" || summary.Checksums == nil {
		t.Fatal("summary DTO changed", summary, err)
	}
	for _, want := range []struct {
		Expected, Status string `json:"-"`
	}{{expected, "matched"}, {strings.ToUpper(expected), "matched"}, {strings.ToUpper(expected[:32]) + expected[32:], "matched"}, {strings.Repeat("0", 64), "mismatch"}} {
		raw := c.call("POST", route, map[string]string{"sha256": want.Expected}, 200)
		decoder := json.NewDecoder(bytes.NewReader(raw))
		found := false
		for {
			var event struct {
				Result struct {
					SHA256 string `json:"sha256"`
					Status string `json:"status"`
					Bytes  int64  `json:"bytes"`
				} `json:"result"`
				Error *presentation.Error `json:"error"`
			}
			err := decoder.Decode(&event)
			if err == io.EOF {
				break
			}
			if err != nil || event.Error != nil {
				t.Fatalf("stream error: %s %v", raw, err)
			}
			if event.Result.SHA256 != "" {
				found = true
				if event.Result.SHA256 != expected || event.Result.Status != want.Status || event.Result.Bytes != int64(len(data)) {
					t.Fatalf("wrong digest: %+v", event.Result)
				}
			}
		}
		if !found {
			t.Fatalf("missing final result: %s", raw)
		}
	}
	c.call("POST", route, map[string]string{"sha256": "not a checksum"}, 400)
	// Direct callers do not have the HTTP controller's lowercase normalization.
	for _, rawExpected := range []string{strings.ToUpper(expected), strings.ToUpper(expected[:32]) + expected[32:]} {
		run, err := objectssvc.PrepareChecksum(ctx, client, bucket, key, rawExpected, true)
		if err != nil {
			t.Fatal(err)
		}
		var value *objectssvc.ChecksumResult
		var streamErr *apperror.Error
		err = run.Stream(ctx, func(event resultdto.StreamEvent[objectssvc.ChecksumResult]) bool {
			if event.Error != nil {
				streamErr = event.Error
			}
			if event.Result != nil {
				value = event.Result
			}
			return true
		})
		run.Close()
		if err != nil || streamErr != nil || value == nil || value.Status != "matched" || value.SHA256 != expected || value.Bytes != int64(len(data)) {
			t.Fatal("service checksum depends on HTTP normalization", value, streamErr, err)
		}
	}
}
func TestObjectUsageVersionsIntegration(t *testing.T) {
	c, client, bucket := fileFixture(t)
	ctx := context.Background()
	if err := client.SetBucketVersioning(ctx, bucket, minio.BucketVersioningConfiguration{Status: "Enabled"}); err != nil {
		t.Fatal(err)
	}
	put := func(key, data string) {
		t.Helper()
		if _, err := client.PutObject(ctx, bucket, key, strings.NewReader(data), int64(len(data)), minio.PutObjectOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	put("root/a/item + # %.txt", "old")
	put("root/a/item + # %.txt", "current")
	put("root/deleted.txt", "gone")
	if err := client.RemoveObject(ctx, bucket, "root/deleted.txt", minio.RemoveObjectOptions{}); err != nil {
		t.Fatal(err)
	}
	put("root/empty/", "")
	put("outside.txt", "ignore")
	var page struct {
		Rows    []objectssvc.ObjectUsageRow `json:"rows"`
		Scanned int                         `json:"scanned"`
	}
	route := "objects/usage?" + url.Values{"bucket": {bucket}, "prefix": {"root/"}, "versions": {"1"}}.Encode()
	if err := json.Unmarshal(c.call("GET", route, nil, 200), &page); err != nil {
		t.Fatal(err)
	}
	totals := map[string]int64{}
	counts := map[string]int{}
	for _, row := range page.Rows {
		if !strings.HasPrefix(row.Key, "root/") {
			t.Fatal("scope leak", row)
		}
		totals[row.Kind] += row.Size
		counts[row.Kind]++
	}
	if totals["current"] != 7 || totals["version"] != 7 || counts["deleted"] != 1 {
		t.Fatal(totals, counts, fmt.Sprint(page.Rows))
	}
}

func TestMultipartPropertyRewriteIntegration(t *testing.T) {
	_, client, bucket := fileFixture(t)
	ctx := context.Background()
	key := "large-copy.bin"
	data := bytes.Repeat([]byte("x"), 11*1024*1024)
	for _, concurrent := range []bool{false, true} {
		t.Run(fmt.Sprint(concurrent), func(t *testing.T) {
			_, err := client.PutObject(ctx, bucket, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			info, err := client.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			copyClient := client
			if concurrent {
				base, err := minio.DefaultTransport(false)
				if err != nil {
					t.Fatal(err)
				}
				calls := 0
				transport := objectToolsTransport(func(req *http.Request) (*http.Response, error) {
					resp, err := base.RoundTrip(req)
					if err == nil && req.URL.Query().Get("partNumber") != "" && req.Method == "PUT" {
						calls++
						if calls == 3 {
							_, e := client.PutObject(ctx, bucket, key, strings.NewReader("new body"), 8, minio.PutObjectOptions{})
							if e != nil {
								t.Error(e)
							}
						}
					}
					return resp, err
				})
				copyClient, err = minio.New("127.0.0.1:19000", &minio.Options{Creds: credentials.NewStaticV4("minio-web-test", "minio-web-test-secret", ""), Region: "us-east-1", Transport: transport, MaxRetries: 1})
				if err != nil {
					t.Fatal(err)
				}
			}
			opts := minio.PutObjectOptions{ContentType: "application/json", UserMetadata: map[string]string{"x-amz-meta-owner": "tools"}}
			opts.SetMatchETag(info.ETag)
			err = objectssvc.MultipartRewrite(ctx, storagesvc.NewS3Client(copyClient), bucket, key, info, opts, 5*1024*1024)
			object, e := client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
			if e != nil {
				t.Fatal(e)
			}
			actual, e := io.ReadAll(object)
			_ = object.Close()
			if e != nil {
				t.Fatal(e)
			}
			if concurrent {
				if err == nil || string(actual) != "new body" {
					t.Fatalf("concurrent file was overwritten: %v %d", err, len(actual))
				}
			} else {
				if err != nil || !bytes.Equal(actual, data) {
					t.Fatalf("multipart body changed: %v %d", err, len(actual))
				}
				changed, e := client.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
				if e != nil || changed.ContentType != "application/json" {
					t.Fatal(changed, e)
				}
			}
		})
	}
}

type objectToolsTransport func(*http.Request) (*http.Response, error)

func (f objectToolsTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestObjectToolsLeastPrivilegeIntegration(t *testing.T) {
	root, client, bucket := fileFixture(t)
	ctx := context.Background()
	key := "limited.txt"
	if err := client.SetBucketVersioning(ctx, bucket, minio.BucketVersioningConfiguration{Status: "Enabled"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.PutObject(ctx, bucket, key, strings.NewReader("body"), 4, minio.PutObjectOptions{ContentType: "text/plain"}); err != nil {
		t.Fatal(err)
	}
	limited := func(actions []string) *apiClient {
		t.Helper()
		doc := map[string]any{"Version": "2012-10-17", "Statement": []any{
			map[string]any{"Effect": "Allow", "Action": []string{"s3:ListBucket"}, "Resource": []string{"arn:aws:s3:::" + bucket}},
			map[string]any{"Effect": "Allow", "Action": actions, "Resource": []string{"arn:aws:s3:::" + bucket + "/*"}},
		}}
		var pair struct{ AccessKey, SecretKey string }
		if err := json.Unmarshal(root.call("POST", "admin/access-keys", map[string]any{"name": "tools-limited", "policy": doc}, 200), &pair); err != nil || pair.AccessKey == "" {
			t.Fatal("missing limited key", err)
		}
		t.Cleanup(func() {
			root.call("DELETE", "admin/access-keys/"+pair.AccessKey, map[string]any{"confirmation": pair.AccessKey}, 200)
		})
		return loginTest(t, root.Origin, pair.AccessKey, pair.SecretKey)
	}
	route := "objects/properties?" + url.Values{"bucket": {bucket}, "key": {key}}.Encode()
	tagger := limited([]string{"s3:GetObject", "s3:GetObjectTagging", "s3:PutObjectTagging"})
	var before presentation.ObjectProperties
	if err := json.Unmarshal(tagger.call("GET", route, nil, 200), &before); err != nil {
		t.Fatal(err)
	}
	tagger.call("PUT", route, map[string]any{"revision": before.Revision, "tags": map[string]string{"owner": "ops"}, "tagsMode": "merge"}, 200)
	var after presentation.ObjectProperties
	if err := json.Unmarshal(tagger.call("GET", route, nil, 200), &after); err != nil {
		t.Fatal(err)
	}
	tagger.call("PUT", route, map[string]any{"revision": after.Revision, "headers": map[string]string{"Content-Type": "application/json"}}, 403)
	info, err := client.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil || info.ContentType != "text/plain" {
		t.Fatal(info, err)
	}
	// This MinIO authorizes versionId tagging with the current tagging actions.
	// Pinning tags must not require GetObjectVersion body access or ListBucketVersions.
	sum := sha256.Sum256([]byte("body"))
	raw := tagger.call("POST", "objects/checksum?"+url.Values{"bucket": {bucket}, "key": {key}}.Encode(), map[string]string{"sha256": hex.EncodeToString(sum[:])}, 200)
	if !bytes.Contains(raw, []byte(`"status":"matched"`)) {
		t.Fatalf("current checksum failed: %s", raw)
	}
	versionOnly := limited([]string{"s3:GetObject", "s3:GetObjectVersionTagging", "s3:PutObjectVersionTagging"})
	var unavailable presentation.ObjectProperties
	if err := json.Unmarshal(versionOnly.call("GET", route, nil, 200), &unavailable); err != nil || unavailable.TagsError == "" || unavailable.Version == "" {
		t.Fatal("native tagging denial hidden", unavailable, err)
	}
	versionOnly.call("PUT", route, map[string]any{"revision": unavailable.Revision, "tags": map[string]string{"bad": "fallback"}}, 403)
	actual, err := client.GetObjectTagging(ctx, bucket, key, minio.GetObjectTaggingOptions{VersionID: unavailable.Version})
	if err != nil || actual.ToMap()["bad"] != "" || actual.ToMap()["owner"] != "ops" {
		t.Fatal("denied tagging changed the checked version", actual, err)
	}
	reader := limited([]string{"s3:GetObject"})
	var readonly presentation.ObjectProperties
	if err := json.Unmarshal(reader.call("GET", route, nil, 200), &readonly); err != nil || readonly.TagsError == "" {
		t.Fatal("unavailable tags hidden", readonly, err)
	}
	reader.call("PUT", route, map[string]any{"revision": readonly.Revision, "tags": map[string]string{"bad": "write"}}, 403)
	var rights struct {
		Known   bool   `json:"known"`
		Allowed []bool `json:"allowed"`
	}
	if err := json.Unmarshal(reader.call("POST", "action-access", map[string]any{"queries": []iam.Query{{Action: "s3:ListBucketVersions", Bucket: bucket}}}, 200), &rights); err != nil || !rights.Known || len(rights.Allowed) != 1 || rights.Allowed[0] {
		t.Fatal("version UI permission", rights, err)
	}
}

func TestObjectUsagePaginationIntegration(t *testing.T) {
	c, client, bucket := fileFixture(t)
	ctx := context.Background()
	if err := client.SetBucketVersioning(ctx, bucket, minio.BucketVersioningConfiguration{Status: "Enabled"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 501; i++ {
		if _, err := client.PutObject(ctx, bucket, fmt.Sprintf("scope/%04d", i), strings.NewReader("x"), 1, minio.PutObjectOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.PutObject(ctx, bucket, "scope/0000", strings.NewReader("xx"), 2, minio.PutObjectOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, versions := range []string{"0", "1"} {
		q := url.Values{"bucket": {bucket}, "prefix": {"scope/"}, "versions": {versions}}
		var page struct {
			Rows    []objectssvc.ObjectUsageRow `json:"rows"`
			Scanned int                         `json:"scanned"`
			Next    string                      `json:"nextToken"`
		}
		if err := json.Unmarshal(c.call("GET", "objects/usage?"+q.Encode(), nil, 200), &page); err != nil {
			t.Fatal(err)
		}
		if page.Scanned != 500 || page.Next == "" {
			t.Fatal("missing first page", page.Scanned, page.Next)
		}
		first := page
		token := page.Next
		q.Set("token", token)
		page.Rows = nil
		if err := json.Unmarshal(c.call("GET", "objects/usage?"+q.Encode(), nil, 200), &page); err != nil {
			t.Fatal(err)
		}
		if page.Next != "" || page.Scanned == 0 {
			t.Fatal("wrong final page", page.Scanned, page.Next)
		}
		counts := map[string]int{}
		sizes := map[string]int64{}
		for _, row := range append(first.Rows, page.Rows...) {
			counts[row.Kind]++
			sizes[row.Kind] += row.Size
		}
		if counts["current"] != 501 || sizes["current"] != 502 {
			t.Fatal(counts, sizes)
		}
		if versions == "1" {
			if counts["version"] != 1 || sizes["version"] != 1 {
				t.Fatal(counts, sizes)
			}
			q.Set("prefix", "other/")
			c.call("GET", "objects/usage?"+q.Encode(), nil, 400)
		}
	}
}
