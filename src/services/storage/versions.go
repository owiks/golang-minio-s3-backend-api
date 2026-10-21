package storage

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/telemetry"
)

var ErrInvalidVersionResponse = errors.New("invalid version listing error response")

type VersionRow struct {
	ID       string    `xml:"VersionId"`
	Key      string    `xml:"Key"`
	Modified time.Time `xml:"LastModified"`
	Size     int64     `xml:"Size"`
	Latest   bool      `xml:"IsLatest"`
	Deleted  bool      `xml:"-"`
}
type VersionPage struct {
	Rows                     []VersionRow
	Truncated                bool
	KeyMarker, VersionMarker string
}

func (v *VersionPage) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	// HTTP 200 can still contain an upstream Error or a proxy document. Only
	// the native listing root may represent a successful (possibly empty) page.
	if start.Name.Local != "ListVersionsResult" {
		return ErrInvalidVersionResponse
	}
	for {
		token, err := d.Token()
		if err != nil {
			return err
		}
		switch x := token.(type) {
		case xml.StartElement:
			switch x.Name.Local {
			case "Version", "DeleteMarker":
				var row VersionRow
				if err := d.DecodeElement(&row, &x); err != nil {
					return err
				}
				row.Deleted = x.Name.Local == "DeleteMarker"
				v.Rows = append(v.Rows, row)
			case "IsTruncated":
				if err := d.DecodeElement(&v.Truncated, &x); err != nil {
					return err
				}
			case "NextKeyMarker":
				if err := d.DecodeElement(&v.KeyMarker, &x); err != nil {
					return err
				}
			case "NextVersionIdMarker":
				if err := d.DecodeElement(&v.VersionMarker, &x); err != nil {
					return err
				}
			default:
				if err := d.Skip(); err != nil {
					return err
				}
			}
		case xml.EndElement:
			if x.Name == start.Name {
				return nil
			}
		}
	}
}

// Decode exactly one complete XML document. Decode alone accepts a valid
// leading element while leaving a second document or corrupt suffix unread.
func decodeVersionPage(reader io.Reader) (VersionPage, error) {
	decoder := xml.NewDecoder(reader)
	var page VersionPage
	seen := false
	first := true
	for {
		token, err := decoder.Token()
		if err == io.EOF && seen {
			return page, nil
		}
		if err != nil {
			return VersionPage{}, ErrInvalidVersionResponse
		}
		switch token := token.(type) {
		case xml.StartElement:
			if seen {
				return VersionPage{}, ErrInvalidVersionResponse
			}
			if err := decoder.DecodeElement(&page, &token); err != nil {
				return VersionPage{}, ErrInvalidVersionResponse
			}
			seen = true
		case xml.CharData:
			text := string(token)
			if first {
				text = strings.TrimPrefix(text, "\ufeff") // Optional UTF-8 BOM at document start.
			}
			if strings.TrimSpace(text) != "" {
				return VersionPage{}, ErrInvalidVersionResponse
			}
		case xml.ProcInst:
			if seen && strings.EqualFold(token.Target, "xml") {
				return VersionPage{}, ErrInvalidVersionResponse
			}
		case xml.Directive, xml.EndElement:
			if seen {
				return VersionPage{}, ErrInvalidVersionResponse
			}
		}
		first = false
	}
}

// Preserve native version markers and a bounded XML response: the streaming SDK
// listing cannot return the exact continuation markers required by the UI.
func ReadVersionPage(ctx context.Context, client *minio.Client, transport http.RoundTripper, bucket, prefix, keyMarker, versionMarker string, maxKeys int) (VersionPage, error) {
	q := url.Values{"versions": {""}, "prefix": {prefix}, "max-keys": {strconv.Itoa(maxKeys)}, "encoding-type": {"url"}}
	if keyMarker != "" {
		q.Set("key-marker", keyMarker)
	}
	if versionMarker != "" {
		q.Set("version-id-marker", versionMarker)
	}
	signed, err := client.Presign(ctx, "GET", bucket, "", time.Minute, q)
	if err != nil {
		return VersionPage{}, err
	}
	request, err := http.NewRequestWithContext(ctx, "GET", signed.String(), nil)
	if err != nil {
		return VersionPage{}, err
	}
	response, err := (s3ResponseTransport{base: telemetry.Transport{Base: transport}}).RoundTrip(request)
	if err != nil {
		return VersionPage{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		var native minio.ErrorResponse
		if xml.NewDecoder(response.Body).Decode(&native) != nil {
			return VersionPage{}, ErrInvalidVersionResponse
		}
		native.StatusCode = response.StatusCode
		return VersionPage{}, native
	}
	return decodeVersionPage(response.Body)
}
