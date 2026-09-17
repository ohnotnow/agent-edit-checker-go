package aec

import (
	"fmt"
	"io"
)

const (
	exitOK      = 0
	exitBlocked = 2
	exitUsage   = 64
)

const usage = `usage: aec <command>

commands:
  hook edit    PreToolUse hook for the Write|Edit matcher (payload on stdin)
  hook bash    PreToolUse hook for the Bash matcher (payload on stdin)
`

// Run dispatches a command line and returns the process exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 2 && args[0] == "hook" {
		switch args[1] {
		case "edit":
			return runHook(hookEdit, stdin, stderr)
		case "bash":
			return runHook(hookBash, stdin, stderr)
		}
	}
	fmt.Fprint(stderr, usage)
	return exitUsage
}
