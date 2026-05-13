//go:build unix

package idempotency

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// lockFile acquires an exclusive advisory lock (LOCK_EX) on f via flock(2).
// The lock is released either by unlockFile or when the file descriptor is
// closed. flock is per-process advisory locking — sufficient for serialising
// cross-process appends because each golink invocation opens its own fd.
func lockFile(f *os.File) error {
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		return fmt.Errorf("flock: %w", err)
	}
	return nil
}

// unlockFile releases the advisory lock on f.
func unlockFile(f *os.File) error {
	if err := unix.Flock(int(f.Fd()), unix.LOCK_UN); err != nil {
		return fmt.Errorf("funlock: %w", err)
	}
	return nil
}
