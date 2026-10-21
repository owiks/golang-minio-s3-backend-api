package objects

import (
	"context"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
)

type BucketRow struct {
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
}

type Entry struct {
	Key      string     `json:"key"`
	Name     string     `json:"name"`
	Kind     string     `json:"kind"`
	Size     int64      `json:"size"`
	Modified *time.Time `json:"modified,omitempty"`
	ETag     string     `json:"etag,omitempty"`
}

// CommonPrefixes have no timestamps. Only an actual object at "folder/"
// can supply a date; never substitute the date of a nested file. Optional
// metadata is bounded to four HEADs at a time and two seconds per page.
func FillFolderDates(parent context.Context, client StreamClient, bucket string, entries []Entry) {
	jobs := make(chan int, len(entries))
	for i := range entries {
		if entries[i].Kind == "folder" {
			jobs <- i
		}
	}
	close(jobs)
	if len(jobs) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	var workers sync.WaitGroup
	for range min(4, len(jobs)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					return
				}
				info, err := client.StatObject(ctx, bucket, entries[i].Key, minio.StatObjectOptions{})
				if err == nil && ctx.Err() == nil && !info.LastModified.IsZero() {
					modified := info.LastModified
					entries[i].Modified = &modified
				}
			}
		}()
	}
	workers.Wait()
}

func PreviewKind(key string) (string, string) {
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(key), "."))
	images := map[string]string{"png": "image/png", "jpg": "image/jpeg", "jpeg": "image/jpeg", "gif": "image/gif", "webp": "image/webp", "avif": "image/avif", "bmp": "image/bmp"}
	if mime := images[ext]; mime != "" {
		return "image", mime
	}
	audio := map[string]string{"mp3": "audio/mpeg", "wav": "audio/wav", "ogg": "audio/ogg", "oga": "audio/ogg", "opus": "audio/ogg", "m4a": "audio/mp4", "m4b": "audio/mp4", "aac": "audio/aac", "flac": "audio/flac", "weba": "audio/webm"}
	if mime := audio[ext]; mime != "" {
		return "audio", mime
	}
	video := map[string]string{"mp4": "video/mp4", "m4v": "video/mp4", "webm": "video/webm", "ogv": "video/ogg", "mov": "video/quicktime"}
	if mime := video[ext]; mime != "" {
		return "video", mime
	}
	if ext == "pdf" {
		return "pdf", "application/pdf"
	}
	for _, text := range strings.Fields("txt md markdown mdown json csv tsv log xml yaml yml toml ini html css js ts svg sql sh py php go conf cfg env properties jsonl ndjson") {
		if ext == text {
			return "text", "text/plain; charset=utf-8"
		}
	}
	return "unsupported", "application/octet-stream"
}
func ObjectParams(b, k string) (string, string, error) {
	if b == "" || k == "" || len(b) > 63 || len(k) > 1024 {
		return "", "", apperror.New(400, "InvalidObject", "Укажите корректный бакет и ключ объекта.")
	}
	return b, k, nil
}

type PreviewData struct {
	ContentType string
	Data        []byte
}
