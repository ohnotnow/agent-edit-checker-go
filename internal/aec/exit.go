package aec

import (
	"errors"
	"fmt"
	"io"
)

// Process exit codes. The hook codes are Claude Code's contract; the rest
// follow the owner's other CLI tools.
const (
	exitOK         = 0
	exitInternal   = 1
	exitBlocked    = 2
	exitUsage      = 64
	exitValidation = 65
	exitNotFound   = 66
)

// cliError carries the exit code a command wants. An empty message means
// the command has already said everything it needs to and exits silently.
type cliError struct {
	code int
	msg  string
}

func (e *cliError) Error() string { return e.msg }

// errBlocked is the silent exit 2 the hooks return after printing their
// Blocked lines.
var errBlocked = &cliError{code: exitBlocked}

func usageErr(format string, a ...any) error {
	return &cliError{code: exitUsage, msg: fmt.Sprintf(format, a...)}
}

func validationErr(format string, a ...any) error {
	return &cliError{code: exitValidation, msg: fmt.Sprintf(format, a...)}
}

func notFoundErr(format string, a ...any) error {
	return &cliError{code: exitNotFound, msg: fmt.Sprintf(format, a...)}
}

// report turns a command's error into stderr output and an exit code.
func report(err error, stderr io.Writer) int {
	if err == nil {
		return exitOK
	}
	var ce *cliError
	if errors.As(err, &ce) {
		if ce.msg != "" {
			fmt.Fprintf(stderr, "aec: %s\n", ce.msg)
		}
		return ce.code
	}
	fmt.Fprintf(stderr, "aec: %v\n", err)
	return exitInternal
}
