package storage

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type attributesDocument struct {
	etag, size, storageClass, partsCount, objectParts, truncated bool
	parts                                                        []int
}
type attributeNode struct {
	name     string
	children []*attributeNode
	text     strings.Builder
}

// validateS3ObjectAttributes runs only for the exact SDK subresource. It remains
// inside the finite metadata budget, before the SDK sees the response bytes.
func validateS3ObjectAttributes(ctx context.Context, method string, u *url.URL, data []byte) error {
	if method != http.MethodGet || !u.Query().Has("attributes") {
		return nil
	}
	_, key, found := strings.Cut(strings.TrimPrefix(u.Path, "/"), "/")
	if !found || key == "" {
		return nil
	}
	maxHeader, markerHeader := "", ""
	if capture, ok := ctx.Value(attributesCaptureKey{}).(*attributesCapture); ok {
		maxHeader = strconv.Itoa(capture.options.MaxParts)
		markerHeader = strconv.Itoa(capture.options.PartNumberMarker)
	}
	doc, err := decodeObjectAttributes(data, maxHeader, markerHeader)
	if err != nil {
		return err
	}
	if capture, ok := ctx.Value(attributesCaptureKey{}).(*attributesCapture); ok {
		capture.mu.Lock()
		if capture.document != nil {
			capture.mu.Unlock()
			return invalidObjectAttributes()
		}
		capture.document = doc
		capture.mu.Unlock()
	}
	return nil
}

// Accept only the AWS GetObjectAttributesOutput and native MinIO
// getObjectAttributesResponse roots, preserving their exact case.
func decodeObjectAttributes(data []byte, maxHeader, markerHeader string) (*attributesDocument, error) {
	bad := func() (*attributesDocument, error) { return nil, invalidObjectAttributes() }
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var root *attributeNode
	stack := []*attributeNode{}
	nodes := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			if root == nil || len(stack) != 0 {
				return bad()
			}
			break
		}
		if err != nil {
			return bad()
		}
		switch token := token.(type) {
		case xml.StartElement:
			nodes++
			if nodes > 20000 || len(stack) >= 8 {
				return bad()
			}
			for _, a := range token.Attr {
				if a.Name.Local == "nil" && a.Value != "false" && a.Value != "0" {
					return bad()
				}
			}
			n := &attributeNode{name: token.Name.Local}
			if len(stack) == 0 {
				if root != nil || n.name != "GetObjectAttributesOutput" && n.name != "getObjectAttributesResponse" {
					return bad()
				}
				root = n
			} else {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) == 0 {
				return bad()
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 {
				if strings.TrimSpace(string(token)) != "" {
					return bad()
				}
			} else {
				current := stack[len(stack)-1]
				if current.text.Len()+len(token) > 8192 {
					return bad()
				}
				current.text.Write(token)
			}
		case xml.Directive:
			return bad()
		case xml.ProcInst:
			if root != nil || !strings.EqualFold(token.Target, "xml") {
				return bad()
			}
		}
	}
	fields, ok := attributeChildren(root)
	if !ok {
		return bad()
	}
	doc := &attributesDocument{}
	if n := fields["ETag"]; n != nil {
		if !attributeScalar(n) || strings.Trim(strings.TrimSpace(n.text.String()), "\"") == "" {
			return bad()
		}
		doc.etag = true
	}
	if n := fields["StorageClass"]; n != nil {
		if !attributeScalar(n) || strings.TrimSpace(n.text.String()) == "" {
			return bad()
		}
		doc.storageClass = true
	}
	if n := fields["ObjectSize"]; n != nil {
		if _, ok := attributeInteger(n, 0, 1<<63-1); !ok {
			return bad()
		}
		doc.size = true
	}
	if n := fields["Checksum"]; n != nil {
		if !validAttributeChecksums(n) {
			return bad()
		}
	}
	if n := fields["ObjectParts"]; n != nil {
		doc.objectParts = true
		// Part repeats; every other field is a singleton.
		children := map[string]*attributeNode{}
		for _, c := range n.children {
			if c.name == "Part" {
				continue
			}
			if children[c.name] != nil {
				return bad()
			}
			children[c.name] = c
		}
		if strings.TrimSpace(n.text.String()) != "" {
			return bad()
		}
		maxParts := 1000
		if maxHeader != "" {
			v, e := strconv.Atoi(maxHeader)
			if e != nil || v < 1 || v > 1000 {
				return bad()
			}
			maxParts = v
		}
		marker := 0
		if markerHeader != "" {
			v, e := strconv.Atoi(markerHeader)
			if e != nil || v < 0 || v > 10000 {
				return bad()
			}
			marker = v
		}
		total := -1
		if p := children["PartsCount"]; p != nil {
			v, ok := attributeInteger(p, 0, 10000)
			if !ok {
				return bad()
			}
			total = int(v)
			doc.partsCount = true
		}
		if p := children["MaxParts"]; p != nil {
			v, ok := attributeInteger(p, 1, int64(maxParts))
			if !ok {
				return bad()
			}
			maxParts = int(v)
		}
		if p := children["PartNumberMarker"]; p != nil {
			v, ok := attributeInteger(p, 0, 10000)
			if !ok || int(v) != marker {
				return bad()
			}
		}
		truncated := false
		if p := children["IsTruncated"]; p != nil {
			doc.truncated = true
			if !attributeScalar(p) {
				return bad()
			}
			switch strings.TrimSpace(p.text.String()) {
			case "true", "1":
				truncated = true
			case "false", "0":
			default:
				return bad()
			}
		}
		previous := marker
		for _, p := range n.children {
			if p.name != "Part" {
				continue
			}
			f, ok := attributeChildren(p)
			if !ok {
				return bad()
			}
			number, ok := attributeInteger(f["PartNumber"], 1, 10000)
			if !ok || int(number) <= previous {
				return bad()
			}
			if _, ok := attributeInteger(f["Size"], 0, 1<<63-1); !ok {
				return bad()
			}
			if !validAttributeChecksums(p) {
				return bad()
			}
			previous = int(number)
			doc.parts = append(doc.parts, previous)
		}
		if len(doc.parts) > maxParts || total >= 0 && len(doc.parts) > total {
			return bad()
		}
		if len(doc.parts) > 0 && !doc.truncated {
			return bad()
		}
		next := 0
		if p := children["NextPartNumberMarker"]; p != nil {
			v, ok := attributeInteger(p, 0, 10000)
			if !ok {
				return bad()
			}
			next = int(v)
		}
		if truncated {
			if len(doc.parts) == 0 || next <= marker || next != previous || next >= 10000 {
				return bad()
			}
			if total >= 0 && len(doc.parts) >= total && marker == 0 {
				return bad()
			}
		} else if next != 0 && (len(doc.parts) == 0 || next != previous) {
			return bad()
		} else if doc.truncated && total >= 0 && len(doc.parts) < total-marker {
			return bad()
		}
	}
	return doc, nil
}

func attributeChildren(n *attributeNode) (map[string]*attributeNode, bool) {
	if n == nil || strings.TrimSpace(n.text.String()) != "" {
		return nil, false
	}
	fields := map[string]*attributeNode{}
	for _, c := range n.children {
		if fields[c.name] != nil {
			return nil, false
		}
		fields[c.name] = c
	}
	return fields, true
}
func attributeScalar(n *attributeNode) bool { return n != nil && len(n.children) == 0 }
func attributeInteger(n *attributeNode, min, max int64) (int64, bool) {
	if !attributeScalar(n) {
		return 0, false
	}
	text := n.text.String()
	if text == "" || strings.TrimSpace(text) != text {
		return 0, false
	}
	// Decimal digits only: reject signs and overflow before SDK's platform int.
	for _, r := range text {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	value, err := strconv.ParseInt(text, 10, 64)
	return value, err == nil && value >= min && value <= max && int64(int(value)) == value
}
func validAttributeChecksums(n *attributeNode) bool {
	if strings.TrimSpace(n.text.String()) != "" {
		return false
	}
	seen := map[string]bool{}
	for _, c := range n.children {
		if !strings.HasPrefix(c.name, "Checksum") {
			continue
		}
		if seen[c.name] || !attributeScalar(c) || len(c.text.String()) > 512 || strings.TrimSpace(c.text.String()) == "" {
			return false
		}
		seen[c.name] = true
	}
	return true
}
