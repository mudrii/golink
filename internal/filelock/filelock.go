// Package filelock provides a tiny cross-process advisory lock helper built on
// flock(2). It is used by the audit, idempotency, and session-refresh paths to
// serialise sidecar mutations across concurrent golink invocations.
//
// Usage:
//
//	closer, err := filelock.Acquire(path)
//	if err != nil { ... }
//	defer closer.Close()
//
// The returned Closer releases the advisory lock and closes the underlying file
// descriptor in a single call. flock locks are per-fd, so distinct fds within
// the same process still block one another — making this helper suitable for
// tests that exercise concurrent goroutines as well as for cross-process
// contention between separate golink CLI invocations.
package filelock

import (
	"fmt"
	"io"
	"os"
)

// Acquire opens path (creating it with 0o600 if absent) and takes an exclusive
// advisory lock on the resulting fd. The returned Closer releases the lock and
// closes the fd; callers MUST defer Close to avoid leaking either resource.
// The lock file itself is not removed — keeping it on disk lets future
// invocations find the same inode without racing on create.
func Acquire(path string) (io.Closer, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("filelock open: %w", err)
	}
	if err := lockFD(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("filelock acquire: %w", err)
	}
	return &handle{f: f}, nil
}

type handle struct {
	f *os.File
}

// Close releases the advisory lock and closes the underlying fd. It is safe to
// call exactly once; subsequent calls return an error from the closed fd.
func (h *handle) Close() error {
	unlockErr := unlockFD(h.f)
	closeErr := h.f.Close()
	if unlockErr != nil {
		return unlockErr
	}
	if closeErr != nil {
		return fmt.Errorf("filelock close: %w", closeErr)
	}
	return nil
}
