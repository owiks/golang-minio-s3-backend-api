package controllers

import (
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type countedBody struct {
	io.Reader
	closed atomic.Int32
}

func (b *countedBody) Close() error { b.closed.Add(1); return nil }

func TestPartBodyNormalClosePreservesReadDeadline(t *testing.T) {
	source := &countedBody{Reader: strings.NewReader("part")}
	var interrupted atomic.Int32
	b := &partBody{ReadCloser: source, interrupt: func() { interrupted.Add(1) }}
	if _, err := io.Copy(io.Discard, b); err != nil {
		t.Fatal(err)
	}
	_ = b.Close()
	_ = b.Close()
	if source.closed.Load() != 1 || interrupted.Load() != 0 {
		t.Fatal("normal completion poisoned a keep-alive connection")
	}
}

func TestPartBodyCancellationInterruptsReadAndClosesOnce(t *testing.T) {
	source := &countedBody{Reader: strings.NewReader("")}
	var interrupted atomic.Int32
	b := &partBody{ReadCloser: source, interrupt: func() { interrupted.Add(1) }}
	var workers sync.WaitGroup
	workers.Add(2)
	go func() { defer workers.Done(); b.InterruptRead(); _ = b.Close() }()
	go func() { defer workers.Done(); _ = b.Close() }()
	workers.Wait()
	if interrupted.Load() != 1 || source.closed.Load() != 1 {
		t.Fatal("body cleanup was not idempotent")
	}
}
