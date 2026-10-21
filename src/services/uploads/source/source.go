// Package source contains the protocol-independent input model for imports.
package source

import (
	"io"
	"net/url"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
)

// File transfers ownership of Body to the caller. A source must bind reads to
// the Open context; Size is -1 when the length is unknown.
type File struct {
	Body        io.ReadCloser `json:"-"`
	Size        int64         `json:"-"`
	ContentType string        `json:"-"`
}

func ParseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" || len(raw) > 8192 || (u.Port() != "" && u.Port() != "443") {
		return nil, apperror.New(400, "InvalidSourceURL", messages.UploadsInvalidSourceURL)
	}
	return u, nil
}
