package filelock_test

import (
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mudrii/golink/internal/filelock"
)

func TestAcquireSerialisesConcurrentCallers(t *testing.T) {
	t.Parallel()

	lockPath := filepath.Join(t.TempDir(), "test.lock")

	const workers = 8
	var (
		active  atomic.Int32
		maxSeen atomic.Int32
		wg      sync.WaitGroup
	)
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			closer, err := filelock.Acquire(lockPath)
			if err != nil {
				t.Errorf("Acquire: %v", err)
				return
			}
			defer func() {
				if err := closer.Close(); err != nil {
					t.Errorf("Close: %v", err)
				}
			}()

			cur := active.Add(1)
			for {
				prev := maxSeen.Load()
				if cur <= prev || maxSeen.CompareAndSwap(prev, cur) {
					break
				}
			}
			// Hold the lock briefly so any racing goroutine that escaped the
			// mutex would have time to bump active beyond 1.
			time.Sleep(2 * time.Millisecond)
			active.Add(-1)
		}()
	}
	wg.Wait()

	if got := maxSeen.Load(); got != 1 {
		t.Fatalf("max concurrent holders: want 1, got %d", got)
	}
}

func TestAcquireReturnsCloserError(t *testing.T) {
	t.Parallel()

	lockPath := filepath.Join(t.TempDir(), "test.lock")
	closer, err := filelock.Acquire(lockPath)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	// Subsequent acquire must succeed once the prior closer released.
	closer2, err := filelock.Acquire(lockPath)
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	if err := closer2.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
