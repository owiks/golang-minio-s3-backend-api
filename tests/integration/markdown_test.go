package integration

import (
	"testing"

	objectssvc "minio-web/api/src/services/objects"
)

func TestMarkdownAliasesAreBoundedTextPreviews(t *testing.T) {
	for _, key := range []string{"docs/README.md", "guide.MARKDOWN", "docs/intro.mdown"} {
		kind, mime := objectssvc.PreviewKind(key)
		if kind != "text" || mime != "text/plain; charset=utf-8" {
			t.Fatalf("unexpected preview for %q: %s %s", key, kind, mime)
		}
	}
}
