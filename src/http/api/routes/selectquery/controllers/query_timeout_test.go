package controllers

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"minio-web/api/src/config"
	"minio-web/api/src/services/runtime"
	"minio-web/api/tests/testsession"
)

type selectDeadlineWriter struct {
	header                 http.Header
	phase                  string
	entered, release       chan struct{}
	enterOnce, releaseOnce sync.Once
	deadlines              atomic.Int32
	writes, flushes        int
	body                   bytes.Buffer
}

func (w *selectDeadlineWriter) Header() http.Header { return w.header }
func (w *selectDeadlineWriter) WriteHeader(int)     {}
func (w *selectDeadlineWriter) block() error {
	w.enterOnce.Do(func() { close(w.entered) })
	<-w.release
	return os.ErrDeadlineExceeded
}
func (w *selectDeadlineWriter) Write(data []byte) (int, error) {
	w.writes++
	if w.phase == "write" || w.phase == "error" && bytes.Contains(data, []byte(`"error"`)) {
		return 0, w.block()
	}
	return w.body.Write(data)
}
func (w *selectDeadlineWriter) FlushError() error {
	w.flushes++
	if w.phase == "flush" {
		return w.block()
	}
	return nil
}
func (w *selectDeadlineWriter) SetWriteDeadline(deadline time.Time) error {
	if !deadline.After(time.Now()) {
		w.deadlines.Add(1)
		w.releaseOnce.Do(func() { close(w.release) })
	}
	return nil
}

func selectRecordsFrame() []byte {
	var header bytes.Buffer
	for _, pair := range [][2]string{{":message-type", "event"}, {":event-type", "Records"}} {
		header.WriteByte(byte(len(pair[0])))
		header.WriteString(pair[0])
		header.WriteByte(7)
		_ = binary.Write(&header, binary.BigEndian, uint16(len(pair[1])))
		header.WriteString(pair[1])
	}
	frame := make([]byte, 12)
	binary.BigEndian.PutUint32(frame, uint32(16+header.Len()+4))
	binary.BigEndian.PutUint32(frame[4:], uint32(header.Len()))
	binary.BigEndian.PutUint32(frame[8:], crc32.ChecksumIEEE(frame[:8]))
	frame = append(frame, header.Bytes()...)
	frame = append(frame, []byte("row\n")...)
	return binary.BigEndian.AppendUint32(frame, crc32.ChecksumIEEE(frame))
}

func selectTimeoutFixture(t *testing.T) (*runtime.State, *runtime.Session, <-chan struct{}) {
	t.Helper()
	cancelled := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			w.Header().Set("Content-Length", "4")
			w.Header().Set("ETag", `"fixture"`)
			w.Header().Set("Last-Modified", time.Unix(100, 0).UTC().Format(http.TimeFormat))
			return
		}
		if r.Method != "POST" || !r.URL.Query().Has("select") {
			t.Error("unexpected upstream operation", r.Method, r.URL)
			w.WriteHeader(500)
			return
		}
		_, _ = w.Write(selectRecordsFrame())
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		once.Do(func() { close(cancelled) })
	}))
	t.Cleanup(server.Close)
	app := runtime.New(config.Default())
	t.Cleanup(app.Close)
	return app, testsession.New(t, server.URL), cancelled
}

func selectTimeoutRequest() *http.Request {
	r := httptest.NewRequest("POST", "/?bucket=bucket&key=file.csv", strings.NewReader(`{"expression":"SELECT * FROM S3Object","input":{"csv":{}},"output":{"csv":{}},"timeoutSeconds":1}`))
	r.Header.Set("Content-Type", "application/json")
	return r
}

func TestSelectDeadlineInterruptsBlockedWriteAndFlush(t *testing.T) {
	for _, phase := range []string{"write", "flush"} {
		t.Run(phase, func(t *testing.T) {
			app, session, cancelled := selectTimeoutFixture(t)
			writer := &selectDeadlineWriter{header: http.Header{}, phase: phase, entered: make(chan struct{}), release: make(chan struct{})}
			defer writer.releaseOnce.Do(func() { close(writer.release) })
			request := selectTimeoutRequest()
			done := make(chan error, 1)
			go func() { done <- HandleSelectObject(app, writer, request, session) }()
			select {
			case <-writer.entered:
			case <-time.After(time.Second):
				t.Fatal("Select did not reach downstream")
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal("second HTTP response attempted", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Select timeout did not interrupt blocked delivery")
			}
			select {
			case <-cancelled:
			case <-time.After(time.Second):
				t.Fatal("Select upstream remained active")
			}
			// Both the query deadline and the common delivery cancellation may
			// set an immediate deadline; neither may perform another Write.
			if deadlines := writer.deadlines.Load(); deadlines < 1 || deadlines > 2 || writer.writes != 1 || strings.Contains(writer.body.String(), `"result"`) || strings.Contains(writer.body.String(), `"error"`) || request.Context().Err() != nil {
				t.Fatal("timeout retried delivery, emitted success or cancelled parent", writer.deadlines.Load(), writer.writes, writer.body.String(), request.Context().Err())
			}
		})
	}
}

func TestSelectReadTimeoutCanStillDeliverTerminalError(t *testing.T) {
	app, session, cancelled := selectTimeoutFixture(t)
	writer := httptest.NewRecorder()
	request := selectTimeoutRequest()
	if err := HandleSelectObject(app, writer, request, session); err != nil {
		t.Fatal("stream returned a second HTTP error", err)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("timed out upstream remained active")
	}
	if writer.Code != 200 || strings.Count(writer.Body.String(), `"error"`) != 1 || !strings.Contains(writer.Body.String(), `"code":"Timeout"`) || strings.Contains(writer.Body.String(), `"result"`) || request.Context().Err() != nil {
		t.Fatal("terminal timeout missing", writer.Code, writer.Body.String(), request.Context().Err())
	}
}

func TestSelectTerminalErrorDeliveryHasBoundedGrace(t *testing.T) {
	app, session, cancelled := selectTimeoutFixture(t)
	writer := &selectDeadlineWriter{header: http.Header{}, phase: "error", entered: make(chan struct{}), release: make(chan struct{})}
	defer writer.releaseOnce.Do(func() { close(writer.release) })
	done := make(chan error, 1)
	go func() { done <- HandleSelectObject(app, writer, selectTimeoutRequest(), session) }()
	select {
	case <-writer.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("terminal error did not reach delivery")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal("second HTTP response attempted", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("terminal error delivery retained long transfer budget")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("upstream still active")
	}
	if writer.writes != 2 || strings.Contains(writer.body.String(), `"result"`) || strings.Contains(writer.body.String(), `"error"`) {
		t.Fatal("blocked error retried or succeeded", writer.writes, writer.body.String())
	}
}
