package selectquery

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/capabilities"
	"minio-web/api/src/services/result"
	"minio-web/api/src/services/storage"
)

type queryReader struct {
	io.Reader
	closeOnce sync.Once
	closed    bool
	stats     *minio.StatsMessage
	close     func()
}

func (r *queryReader) Close() error {
	r.closeOnce.Do(func() {
		r.closed = true
		if r.close != nil {
			r.close()
		}
	})
	return nil
}
func (r *queryReader) FinalStats() *minio.StatsMessage { return r.stats }

type queryClient struct {
	reader       storage.SelectStream
	openErr      error
	info         minio.ObjectInfo
	changed      bool
	stats, opens int
	ctx          context.Context
	options      minio.SelectObjectOptions
}

func (c *queryClient) StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error) {
	c.stats++
	v := c.info
	if c.changed && c.stats > 1 {
		v.ETag = "replaced"
	}
	return v, nil
}
func (c *queryClient) OpenSelect(ctx context.Context, _, _ string, o minio.SelectObjectOptions) (storage.SelectStream, error) {
	c.opens++
	c.ctx = ctx
	c.options = o
	return c.reader, c.openErr
}

type queryObserver struct{ values []capabilities.Observation }

func (o *queryObserver) Observe(_ context.Context, v capabilities.Observation) {
	o.values = append(o.values, v)
}
func csvQuery() Request {
	return Request{Expression: "SELECT s.name FROM S3Object s WHERE s.id > 1", Input: Input{CSV: &CSVInput{Header: "USE"}}, Output: Output{JSON: &JSONOutput{RecordDelimiter: "\n"}}}
}

func TestValidationRejectsBeforeUpstream(t *testing.T) {
	for name, change := range map[string]func(*Request){"empty SQL": func(r *Request) { r.Expression = " " }, "SQL limit": func(r *Request) { r.Expression = strings.Repeat("x", MaxExpressionBytes+1) }, "two inputs": func(r *Request) { r.Input.JSON = &JSONInput{Type: "LINES"} }, "no output": func(r *Request) { r.Output = Output{} }, "bad CSV header": func(r *Request) { r.Input.CSV.Header = "AUTO" }, "delimiter": func(r *Request) { r.Input.CSV.FieldDelimiter = "::" }, "json mode": func(r *Request) { r.Input = Input{JSON: &JSONInput{Type: "ANY"}} }, "timeout": func(r *Request) { r.TimeoutSeconds = 31 }, "result": func(r *Request) { r.MaxResultBytes = MaxResultBytes + 1 }} {
		t.Run(name, func(t *testing.T) {
			r := csvQuery()
			change(&r)
			client := &queryClient{}
			run, err := Prepare(context.Background(), client, nil, "bucket", "file.csv", r)
			if err == nil || run != nil || client.opens != 0 || client.stats != 0 {
				t.Fatal(run, err, client)
			}
		})
	}
}

func TestStreamConcatenatesBinaryChunksAndPublishesStatsOnlyAtCompletion(t *testing.T) {
	data := bytes.Repeat([]byte("я🙂\n"), 10000)
	reader := &queryReader{Reader: bytes.NewReader(data), stats: &minio.StatsMessage{BytesScanned: 1234567, BytesProcessed: 1234567, BytesReturned: int64(len(data))}}
	client := &queryClient{reader: reader, info: minio.ObjectInfo{Size: 1234567, ETag: "source"}}
	observer := &queryObserver{}
	run, err := Prepare(context.Background(), client, observer, "bucket", "file.csv", csvQuery())
	if err != nil {
		t.Fatal(err)
	}
	defer run.Close()
	var decoded bytes.Buffer
	var final *Result
	events := 0
	err = run.Stream(context.Background(), func(event result.StreamEvent[Result]) bool {
		events++
		if event.Data != nil {
			if event.Data.Encoding != "base64" || final != nil {
				t.Error("bad chunk envelope")
			}
			value, err := base64.StdEncoding.DecodeString(event.Data.Bytes)
			if err != nil || len(value) > ChunkBytes {
				t.Fatal(err, len(value))
			}
			decoded.Write(value)
		}
		if event.Result != nil {
			final = event.Result
		}
		return true
	})
	if err != nil || !bytes.Equal(data, decoded.Bytes()) || final == nil || !final.Complete || final.Bytes != int64(len(data)) || final.Statistics.BytesReturned != int64(len(data)) || !reader.closed || events < 2 || client.opens != 1 || client.stats != 2 || len(observer.values) != 1 || observer.values[0].Err != nil {
		t.Fatal(err, final, reader.closed, events, client, observer)
	}
	if client.options.InputSerialization.CSV.FileHeaderInfo != "USE" || client.options.Expression != csvQuery().Expression {
		t.Fatal("SDK input changed", client.options)
	}
	if err := run.Stream(context.Background(), func(result.StreamEvent[Result]) bool { t.Error("second stream emitted"); return true }); apperror.Public(err).Code != "SelectClosed" {
		t.Fatal(err)
	}
}

func TestPartialErrorsCannotProduceTerminalSuccess(t *testing.T) {
	for _, kind := range []string{"limit", "read error", "changed", "cancelled writer"} {
		t.Run(kind, func(t *testing.T) {
			data := bytes.Repeat([]byte("x"), ChunkBytes*3)
			reader := &queryReader{Reader: bytes.NewReader(data)}
			client := &queryClient{reader: reader, info: minio.ObjectInfo{Size: 1, ETag: "source"}}
			r := csvQuery()
			if kind == "limit" {
				r.MaxResultBytes = ChunkBytes + 1
			}
			if kind == "read error" {
				reader.Reader = io.MultiReader(bytes.NewReader(data[:ChunkBytes]), failingReader{})
			}
			if kind == "changed" {
				client.changed = true
			}
			run, err := Prepare(context.Background(), client, nil, "bucket", "file.csv", r)
			if err != nil {
				t.Fatal(err)
			}
			defer run.Close()
			chunks, terminal := 0, false
			err = run.Stream(context.Background(), func(v result.StreamEvent[Result]) bool {
				if v.Data != nil {
					chunks++
				}
				terminal = terminal || v.Result != nil
				return kind != "cancelled writer"
			})
			if err == nil || terminal || chunks == 0 || !reader.closed {
				t.Fatal(err, terminal, chunks, reader.closed)
			}
		})
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("upstream secret SQL must not leak")
}

func TestUnsupportedDeniedAndUnknownAreObservedWithoutLocalFallback(t *testing.T) {
	for _, err := range []error{minio.ErrorResponse{Code: "NotImplemented", StatusCode: 501}, minio.ErrorResponse{Code: "AccessDenied", StatusCode: 403}, context.DeadlineExceeded} {
		client := &queryClient{openErr: err, info: minio.ObjectInfo{Size: 1}}
		o := &queryObserver{}
		run, got := Prepare(context.Background(), client, o, "bucket", "file.csv", csvQuery())
		if run != nil || got == nil || client.opens != 1 || client.stats != 1 || len(o.values) != 1 || o.values[0].Err != err {
			t.Fatal(run, got, client, o)
		}
	}
}

func TestOversizedSourceAndFirstErrorStayPreflight(t *testing.T) {
	for _, oversized := range []bool{true, false} {
		reader := &queryReader{Reader: failingReader{}}
		client := &queryClient{reader: reader, info: minio.ObjectInfo{Size: 1}}
		if oversized {
			client.info.Size = MaxSourceBytes + 1
		}
		run, err := Prepare(context.Background(), client, nil, "bucket", "file.csv", csvQuery())
		if run != nil || err == nil || oversized && client.opens != 0 || !oversized && !reader.closed {
			t.Fatal(run, err, client, reader.closed)
		}
	}
}

func TestCancellationClosesPreparedRunAndDeadlineIsBounded(t *testing.T) {
	reader := &queryReader{Reader: strings.NewReader("row")}
	client := &queryClient{reader: reader, info: minio.ObjectInfo{Size: 1}}
	r := csvQuery()
	r.TimeoutSeconds = 1
	run, err := Prepare(context.Background(), client, nil, "bucket", "file.csv", r)
	if err != nil {
		t.Fatal(err)
	}
	if deadline, ok := client.ctx.Deadline(); !ok || time.Until(deadline) > time.Second {
		t.Fatal("timeout not propagated")
	}
	run.Close()
	run.Close()
	if !reader.closed || client.ctx.Err() == nil {
		t.Fatal("prepared read leaked")
	}
	if err := run.Stream(context.Background(), func(result.StreamEvent[Result]) bool { return true }); apperror.Public(err).Code != "SelectClosed" {
		t.Fatal(err)
	}
}

func TestEmptyRunAndInvalidStatisticsDoNotSucceed(t *testing.T) {
	var empty Run
	if err := empty.Stream(context.Background(), func(result.StreamEvent[Result]) bool { t.Error("empty run emitted"); return true }); apperror.Public(err).Code != "SelectClosed" {
		t.Fatal(err)
	}
	reader := &queryReader{Reader: strings.NewReader("row"), stats: &minio.StatsMessage{BytesReturned: 10}}
	client := &queryClient{reader: reader, info: minio.ObjectInfo{Size: 3}}
	run, err := Prepare(context.Background(), client, nil, "bucket", "file.csv", csvQuery())
	if err != nil {
		t.Fatal(err)
	}
	defer run.Close()
	err = run.Stream(context.Background(), func(v result.StreamEvent[Result]) bool {
		if v.Result != nil {
			t.Error("inconsistent stats succeeded")
		}
		return true
	})
	if apperror.Public(err).Code != "InvalidSelectStatistics" {
		t.Fatal(err)
	}
}
