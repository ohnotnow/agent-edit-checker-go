//go:build !windows

package aec

import (
	"os"
	"syscall"
)

// lockFile takes an exclusive lock on f, waiting for other holders, since
// parallel tool calls run the hook concurrently.
func lockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

func unlockFile(f *os.File) {
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
