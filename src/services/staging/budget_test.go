package staging

import (
	"io"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestSharedBudgetAndBoundedCopy(t *testing.T) {
	b := New(12)
	f, err := b.Create(8, "budget-test-*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.Create(5, "budget-test-*"); err == nil {
		t.Fatal("oversubscription")
	}
	if _, err = io.Copy(f, strings.NewReader("123456789")); err == nil {
		t.Fatal("copy bypassed reservation")
	}
	info, err := os.Stat(f.Name())
	if err != nil || info.Size() > 8 {
		t.Fatal("unreserved bytes written", err)
	}
	name := f.Name()
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if b.Used() != 0 {
		t.Fatal("reservation leaked")
	}
	if _, err = os.Stat(name); !os.IsNotExist(err) {
		t.Fatal("file not removed")
	}
}
func TestConcurrentBudget(t *testing.T) {
	b := New(8)
	var wg sync.WaitGroup
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := b.Reserve(3)
			if err == nil {
				if b.Used() > 8 {
					t.Error("budget exceeded")
				}
				release()
				release()
			}
		}()
	}
	wg.Wait()
	if b.Used() != 0 {
		t.Fatal("budget leaked")
	}
}
