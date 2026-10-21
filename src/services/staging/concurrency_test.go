package staging

import (
	"io"
	"os"
	"sync"
	"sync/atomic"
	"testing"
)

func TestFileConcurrentWritesCannotExceedReservation(t *testing.T) {
	for round := 0; round < 25; round++ {
		budget := New(1)
		file, err := budget.Create(1, "staging-atomic-*")
		if err != nil {
			t.Fatal(err)
		}
		var jobs sync.WaitGroup
		var success atomic.Int32
		start := make(chan struct{})
		for range 128 {
			jobs.Go(func() {
				<-start
				if n, err := file.Write([]byte("x")); n == 1 && err == nil {
					success.Add(1)
				}
			})
		}
		close(start)
		jobs.Wait()
		info, err := os.Stat(file.Name())
		if err != nil || info.Size() != 1 || success.Load() != 1 || budget.Used() != 1 {
			_ = file.Close()
			t.Fatal("file exceeded its reservation", success.Load(), info, err)
		}
		_ = file.Close()
		if budget.Used() != 0 {
			t.Fatal("reservation retained after cleanup")
		}
	}
}

func TestFileConcurrentSeekReadWriteAndCloseStayBounded(t *testing.T) {
	budget := New(32)
	file, err := budget.Create(32, "staging-position-*")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var jobs sync.WaitGroup
	for range 8 {
		jobs.Go(func() {
			for range 100 {
				_, _ = file.Seek(0, io.SeekEnd)
				_, _ = file.Write([]byte("x"))
				_, _ = file.Seek(0, io.SeekStart)
				_, _ = file.Read(make([]byte, 1))
				_, _ = file.ReadAt(make([]byte, 1), 0)
			}
		})
	}
	jobs.Wait()
	info, err := os.Stat(file.Name())
	if err != nil || info.Size() > 32 {
		t.Fatal("position updates bypassed byte limit", info, err)
	}
	for range 10 {
		jobs.Go(func() { _ = file.Close(); _, _ = file.Write([]byte("x")); _, _ = file.Seek(0, io.SeekStart) })
	}
	jobs.Wait()
	if budget.Used() != 0 {
		t.Fatal("concurrent Close retained reservation")
	}
}
