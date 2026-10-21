package storage

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/xml"
	"hash/crc32"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"

	"minio-web/api/src/services/apperror"
)

const (
	maxSelectFrame        = 1 << 20
	maxSelectHeaders      = 8 << 10
	maxSelectServiceEvent = 64 << 10
	maxSelectUpstream     = 64 << 20
	maxSelectEvents       = 8192
)

type selectScopeKey struct{}
type selectScope struct{ body atomic.Pointer[selectBody] }

// Only the exact signed SDK operation receives streaming treatment. Content
// type never grants a bypass to other POSTs, bucket operations or subresources.
func selectResponse(ctx context.Context, method string, u *url.URL, response *http.Response) (*http.Response, bool, error) {
	if method != http.MethodPost || response.StatusCode != http.StatusOK {
		return response, false, nil
	}
	bucket, key, object := strings.Cut(strings.TrimPrefix(u.Path, "/"), "/")
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || bucket == "" || !object || key == "" || len(q) != 2 || len(q["select"]) != 1 || q.Get("select") != "" || len(q["select-type"]) != 1 || q.Get("select-type") != "2" {
		return response, false, nil
	}
	if _, already := response.Body.(*selectBody); already {
		return response, true, nil
	}
	if response.ContentLength > maxSelectUpstream {
		_ = response.Body.Close()
		return nil, true, selectBoundError()
	}
	body := &selectBody{ctx: ctx, source: response.Body}
	response.Body = body
	if scope, ok := ctx.Value(selectScopeKey{}).(*selectScope); ok {
		scope.body.Store(body)
	}
	return response, true, nil
}

func selectBoundError() error {
	return apperror.New(502, "SelectUpstreamLimit", "Поток S3 Select превысил допустимые границы. Результат неполный.")
}
func invalidSelectEvent() error {
	return apperror.New(502, "InvalidSelectEvent", "S3 Select вернул повреждённое или незавершённое событие. Результат неполный.")
}

// Each event is validated and bounded before the SDK decoder can allocate from
// untrusted lengths. A complete frame is at most 1 MiB; the full result is never
// buffered. Validate End's CRC here because the SDK closes on End before CRC.
type selectBody struct {
	ctx       context.Context
	source    io.ReadCloser
	pending   []byte
	failure   error
	read      int64
	events    int
	ended     atomic.Bool
	stats     atomic.Bool
	closeOnce sync.Once
	closeErr  error
}

func (b *selectBody) Close() error {
	b.closeOnce.Do(func() { b.closeErr = b.source.Close() })
	return b.closeErr
}
func (b *selectBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	if len(b.pending) == 0 {
		if b.failure != nil {
			return 0, b.failure
		}
		if b.ended.Load() {
			return 0, io.EOF
		}
		frame, err := b.frame()
		if err != nil {
			b.failure = err
			_ = b.Close()
			return 0, err
		}
		b.pending = frame
	}
	n := copy(p, b.pending)
	b.pending = b.pending[n:]
	return n, nil
}

func (b *selectBody) frame() ([]byte, error) {
	var prelude [12]byte
	if _, err := io.ReadFull(b.source, prelude[:]); err != nil {
		if cause := b.ctx.Err(); cause != nil {
			return nil, cause
		}
		return nil, invalidSelectEvent()
	}
	total, headers := int64(binary.BigEndian.Uint32(prelude[:4])), int64(binary.BigEndian.Uint32(prelude[4:8]))
	if total < 16 || total > maxSelectFrame || headers > maxSelectHeaders || headers > total-16 {
		return nil, selectBoundError()
	}
	if crc32.ChecksumIEEE(prelude[:8]) != binary.BigEndian.Uint32(prelude[8:]) {
		return nil, invalidSelectEvent()
	}
	b.events++
	b.read += total
	if b.events > maxSelectEvents || b.read > maxSelectUpstream {
		return nil, selectBoundError()
	}
	frame := make([]byte, int(total))
	copy(frame, prelude[:])
	if _, err := io.ReadFull(b.source, frame[12:]); err != nil {
		if cause := b.ctx.Err(); cause != nil {
			return nil, cause
		}
		return nil, invalidSelectEvent()
	}
	if crc32.ChecksumIEEE(frame[:len(frame)-4]) != binary.BigEndian.Uint32(frame[len(frame)-4:]) {
		return nil, invalidSelectEvent()
	}
	h, err := selectEventHeaders(frame[12 : 12+int(headers)])
	if err != nil {
		return nil, err
	}
	payload := frame[12+int(headers) : len(frame)-4]
	if h[":message-type"] == "error" {
		// Do not let SDK error strings expose an upstream message or SQL/data.
		if len(payload) > maxSelectServiceEvent {
			return nil, selectBoundError()
		}
		code := h[":error-code"]
		if code == "" {
			return nil, invalidSelectEvent()
		}
		switch code {
		case "AccessDenied":
			return nil, apperror.New(403, "AccessDenied", "Недостаточно прав MinIO для S3 Select.")
		case "NotImplemented", "UnsupportedOperation":
			return nil, apperror.New(501, "NotImplemented", "S3 Select не поддерживается хранилищем.")
		case "InternalError", "ServiceUnavailable", "SlowDown":
			return nil, apperror.New(502, "SelectStreamFailed", "Хранилище прервало S3 Select. Результат неполный.")
		default:
			return nil, apperror.New(400, "SelectQueryFailed", "Хранилище отклонило запрос S3 Select. Проверьте SQL и формат файла; результат неполный.")
		}
	}
	if h[":message-type"] != "event" {
		return nil, invalidSelectEvent()
	}
	switch h[":event-type"] {
	case "Records":
		if b.stats.Load() {
			return nil, invalidSelectEvent()
		}
	case "Stats", "Progress":
		if h[":content-type"] != "text/xml" || len(payload) > maxSelectServiceEvent {
			return nil, invalidSelectEvent()
		}
		var value struct {
			XMLName   xml.Name
			Scanned   *int64 `xml:"BytesScanned"`
			Processed *int64 `xml:"BytesProcessed"`
			Returned  *int64 `xml:"BytesReturned"`
		}
		// Match the official SDK statistics model and reject missing fields or
		// trailing XML instead of treating an empty decode as real zero usage.
		d := xml.NewDecoder(bytes.NewReader(payload))
		if d.Decode(&value) != nil || value.XMLName.Local != h[":event-type"] || value.Scanned == nil || value.Processed == nil || value.Returned == nil || *value.Scanned < 0 || *value.Processed < 0 || *value.Returned < 0 {
			return nil, invalidSelectEvent()
		}
		for {
			token, e := d.Token()
			if e == io.EOF {
				break
			}
			if e != nil {
				return nil, invalidSelectEvent()
			}
			if text, ok := token.(xml.CharData); !ok || strings.TrimSpace(string(text)) != "" {
				return nil, invalidSelectEvent()
			}
		}
		if h[":event-type"] == "Stats" {
			if b.stats.Load() {
				return nil, invalidSelectEvent()
			}
			b.stats.Store(true)
		}
	case "Cont":
		if len(payload) != 0 {
			return nil, invalidSelectEvent()
		}
	case "End":
		if len(payload) != 0 {
			return nil, invalidSelectEvent()
		}
		b.ended.Store(true)
	default:
		return nil, invalidSelectEvent()
	}
	return frame, nil
}

func selectEventHeaders(data []byte) (map[string]string, error) {
	values := make(map[string]string, 4)
	for len(data) > 0 {
		n := int(data[0])
		data = data[1:]
		if n == 0 || len(data) < n+3 {
			return nil, invalidSelectEvent()
		}
		name := string(data[:n])
		data = data[n:]
		switch name {
		case ":message-type", ":event-type", ":content-type", ":error-code", ":error-message":
		default:
			// The SDK canonicalizes names after removing ':'. Do not permit
			// alias/case variants that could override the validated headers.
			return nil, invalidSelectEvent()
		}
		if data[0] != 7 {
			return nil, invalidSelectEvent()
		}
		size := int(binary.BigEndian.Uint16(data[1:3]))
		data = data[3:]
		if len(data) < size || len(values) >= 16 {
			return nil, invalidSelectEvent()
		}
		if _, exists := values[name]; exists {
			return nil, invalidSelectEvent()
		}
		values[name] = string(data[:size])
		data = data[size:]
	}
	return values, nil
}
