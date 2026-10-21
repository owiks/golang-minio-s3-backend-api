package storage

import (
	"bufio"
	"encoding/json"
	"io"
	"sync"

	"minio-web/api/src/services/apperror"
)

const maxDiagnosticRecord = 1 << 20

// Frame one JSON value before the SDK sees it. Long subscriptions are allowed,
// but individual values cannot grow its decoder without a byte bound.
// Whitespace heartbeats outside values are discarded without accumulating.
type diagnosticBody struct {
	source    io.ReadCloser
	input     *bufio.Reader
	pending   []byte
	failure   error
	onFailure func(error)
	closeOnce sync.Once
	closeErr  error
}

func newDiagnosticBody(source io.ReadCloser, onFailure func(error)) *diagnosticBody {
	return &diagnosticBody{source: source, input: bufio.NewReaderSize(source, 4096), onFailure: onFailure}
}

func (b *diagnosticBody) Close() error {
	b.closeOnce.Do(func() { b.closeErr = b.source.Close() })
	return b.closeErr
}

func jsonSpace(c byte) bool { return c == ' ' || c == '\n' || c == '\r' || c == '\t' }

func (b *diagnosticBody) record() ([]byte, error) {
	var first byte
	for {
		c, err := b.input.ReadByte()
		if err != nil {
			return nil, err
		}
		if !jsonSpace(c) {
			first = c
			break
		}
	}
	value := make([]byte, 0, 4096)
	depth, quoted, escaped := 0, false, false
	container := first == '{' || first == '['
	stringValue := first == '"'
	for c := first; ; {
		value = append(value, c)
		if len(value) > maxDiagnosticRecord {
			return nil, apperror.New(502, "DiagnosticRecordTooLarge", "Запись диагностики MinIO превышает 1 МиБ. Сбор остановлен.")
		}
		if quoted {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
		} else {
			switch c {
			case '"':
				quoted = true
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
		if container && depth == 0 && !quoted || stringValue && len(value) > 1 && !quoted {
			break
		}
		next, err := b.input.ReadByte()
		if err != nil {
			if err == io.EOF && !container && !stringValue {
				break
			}
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return nil, err
		}
		if !container && !stringValue && jsonSpace(next) {
			break
		}
		c = next
	}
	if !json.Valid(value) {
		return nil, apperror.New(502, "InvalidDiagnosticRecord", "MinIO вернул некорректную запись диагностики. Сбор остановлен.")
	}
	return append(value, '\n'), nil
}

func (b *diagnosticBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(b.pending) == 0 {
		if b.failure != nil {
			return 0, b.failure
		}
		value, err := b.record()
		if err != nil {
			b.failure = err
			if err != io.EOF {
				if b.onFailure != nil {
					b.onFailure(err)
				}
			}
			// Cancel the SDK scope first on failure; never drain rejected bytes.
			// Also close at EOF: the SDK log reconnect path omits Body.Close.
			_ = b.Close()
			return 0, err
		}
		b.pending = value
	}
	n := copy(p, b.pending)
	b.pending = b.pending[n:]
	return n, nil
}
