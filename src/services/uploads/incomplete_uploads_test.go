package uploads

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
)

type partsPages struct {
	pages   []minio.ListObjectPartsResult
	markers []int
}

func (p *partsPages) ListObjectParts(_ context.Context, _, _, _ string, marker, maxParts int) (minio.ListObjectPartsResult, error) {
	p.markers = append(p.markers, marker)
	if len(p.markers) > len(p.pages) {
		return minio.ListObjectPartsResult{}, fmt.Errorf("unexpected extra page")
	}
	return p.pages[len(p.markers)-1], nil
}
func part(n int, size int64) minio.ObjectPart { return minio.ObjectPart{PartNumber: n, Size: size} }
func TestInspectPartsRejectsInvalidPaginationWithoutSnapshot(t *testing.T) {
	first := minio.ListObjectPartsResult{ObjectParts: []minio.ObjectPart{part(1, 7)}, IsTruncated: true, NextPartNumberMarker: 1}
	for _, tc := range []struct {
		name  string
		pages []minio.ListObjectPartsResult
	}{
		{"duplicate across pages", []minio.ListObjectPartsResult{first, {ObjectParts: []minio.ObjectPart{part(1, 7)}}}},
		{"duplicate in page", []minio.ListObjectPartsResult{{ObjectParts: []minio.ObjectPart{part(1, 7), part(1, 7)}}}},
		{"descending", []minio.ListObjectPartsResult{{ObjectParts: []minio.ObjectPart{part(2, 7), part(1, 7)}}}},
		{"zero part", []minio.ListObjectPartsResult{{ObjectParts: []minio.ObjectPart{part(0, 7)}}}},
		{"oversized number", []minio.ListObjectPartsResult{{ObjectParts: []minio.ObjectPart{part(10001, 7)}}}},
		{"negative size", []minio.ListObjectPartsResult{{ObjectParts: []minio.ObjectPart{part(1, -1)}}}},
		{"overflow", []minio.ListObjectPartsResult{{ObjectParts: []minio.ObjectPart{part(1, math.MaxInt64), part(2, 1)}}}},
		{"stuck marker", []minio.ListObjectPartsResult{{ObjectParts: []minio.ObjectPart{part(1, 7)}, IsTruncated: true}}},
		{"skipped marker", []minio.ListObjectPartsResult{{ObjectParts: []minio.ObjectPart{part(1, 7)}, IsTruncated: true, NextPartNumberMarker: 2}}},
		{"empty truncated", []minio.ListObjectPartsResult{{IsTruncated: true, NextPartNumberMarker: 1}}},
		{"overfull page", []minio.ListObjectPartsResult{{ObjectParts: make([]minio.ObjectPart, 1001)}}},
		{"truncated at final number", []minio.ListObjectPartsResult{{ObjectParts: []minio.ObjectPart{part(10000, 7)}, IsTruncated: true, NextPartNumberMarker: 10000}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &partsPages{pages: tc.pages}
			got, err := InspectParts(context.Background(), p, "bucket", "key", "id")
			if err == nil || apperror.Public(err).Code != "InvalidUploadPartsResponse" || got.Revision != "" || got.Count != 0 || got.Size != 0 {
				t.Fatalf("published invalid snapshot: %+v %v", got, err)
			}
		})
	}
}
func TestInspectPartsAcceptsEmptySparseAndFullListings(t *testing.T) {
	for _, count := range []int{0, 2, 10000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			p := &partsPages{}
			if count == 0 {
				p.pages = append(p.pages, minio.ListObjectPartsResult{})
			}
			for i := 0; i < count; i += 1000 {
				end := min(i+1000, count)
				page := minio.ListObjectPartsResult{IsTruncated: end < count, NextPartNumberMarker: end}
				for n := i + 1; n <= end; n++ {
					number := n
					if count == 2 {
						number = n * 3
					}
					page.ObjectParts = append(page.ObjectParts, part(number, 7))
				}
				p.pages = append(p.pages, page)
			}
			got, err := InspectParts(context.Background(), p, "bucket", "key", "id")
			if err != nil || got.Count != count || got.Size != int64(count)*7 || got.Revision == "" {
				t.Fatalf("valid listing rejected: %+v %v", got, err)
			}
			for i, marker := range p.markers {
				if marker != i*1000 {
					t.Fatal("incorrect continuation", p.markers)
				}
			}
		})
	}
}
