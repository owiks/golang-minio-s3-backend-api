package zipread

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
)

func TestZIPDirectoryLimitsBeforeOpening(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Create("folder/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write([]byte("body")); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	original := buf.Bytes()
	for _, kind := range []string{"valid", "short", "entry-limit", "entry-count-mismatch", "central-limit", "zip64-offset", "split-volume"} {
		t.Run(kind, func(t *testing.T) {
			data := bytes.Clone(original)
			end := len(data) - 22
			switch kind {
			case "short":
				data = data[:21]
			case "entry-limit":
				binary.LittleEndian.PutUint16(data[end+10:], 10001)
			case "entry-count-mismatch":
				binary.LittleEndian.PutUint16(data[end+10:], 0)
			case "central-limit":
				binary.LittleEndian.PutUint32(data[end+12:], 16*1024*1024+1)
			case "zip64-offset":
				binary.LittleEndian.PutUint32(data[end+16:], 0xffffffff)
			case "split-volume":
				binary.LittleEndian.PutUint16(data[end+4:], 1)
			}
			reader, err := CheckedZIP(bytes.NewReader(data), int64(len(data)))
			if kind == "valid" {
				if err != nil || len(reader.File) != 1 || reader.File[0].Name != "folder/file.txt" {
					t.Fatal("valid ZIP rejected", err)
				}
				return
			}
			expected := "ZIPLimit"
			if kind == "short" || kind == "entry-count-mismatch" {
				expected = "InvalidZIP"
			}
			if err == nil || apperror.Public(err).Code != expected {
				t.Fatal("unsafe ZIP accepted", kind, err)
			}
		})
	}
}
