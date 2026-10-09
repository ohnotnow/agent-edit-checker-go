//go:build windows

package aec

import "os"

// lockFile does nothing on Windows: concurrent hook calls there may lose
// a count, which only delays a nudge.
func lockFile(*os.File) error { return nil }

func unlockFile(*os.File) {}
