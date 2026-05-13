//go:build unix

package filelock

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// lockFD takes an exclusive advisory lock (LOCK_EX) on f via flock(2). The
// lock is released either by unlockFD or when the file descriptor is closed.
// flock is per-fd advisory locking — sufficient for serialising cross-process
// sidecar mutations because each golink invocation opens its own fd.
func lockFD(f *os.File) error {
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		return fmt.Errorf("flock: %w", err)
	}
	return nil
}

// unlockFD releases the advisory lock on f.
func unlockFD(f *os.File) error {
	if err := unix.Flock(int(f.Fd()), unix.LOCK_UN); err != nil {
		return fmt.Errorf("funlock: %w", err)
	}
	return nil
}
