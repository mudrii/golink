//go:build unix

package audit

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// lockFile acquires an exclusive advisory lock (LOCK_EX) on f via flock(2).
// The lock is released either by unlockFile or when the file descriptor is
// closed. flock is a per-process advisory lock — sufficient for serialising
// cross-process appends because each golink CLI invocation opens its own fd.
func lockFile(f *os.File) error {
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		return fmt.Errorf("flock: %w", err)
	}
	return nil
}

// unlockFile releases the advisory lock on f. Errors are returned so the
// caller can surface them, but in practice the lock is also released on
// fd close so failure here is rarely fatal.
func unlockFile(f *os.File) error {
	if err := unix.Flock(int(f.Fd()), unix.LOCK_UN); err != nil {
		return fmt.Errorf("funlock: %w", err)
	}
	return nil
}
