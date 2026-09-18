package aec

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// runOut is run without the empty-stdout check, for commands that print.
func runOut(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(args, strings.NewReader(stdin), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// withCommand registers a command for one test.
func withCommand(t *testing.T, name string, cmd command) {
	t.Helper()
	old, had := commands[name]
	commands[name] = cmd
	t.Cleanup(func() {
		if had {
			commands[name] = old
		} else {
			delete(commands, name)
		}
	})
}

func TestHelp(t *testing.T) {
	for _, arg := range []string{"help", "--help", "-h"} {
		code, stdout, stderr := runOut(t, "", arg)
		if code != 0 || !strings.HasPrefix(stdout, "usage:") || stderr != "" {
			t.Errorf("%s: code=%d stdout=%q stderr=%q", arg, code, stdout, stderr)
		}
	}
}

func TestRunErrorMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code int
		want string
	}{
		{"ok", nil, 0, ""},
		{"usage", usageErr("bad %s", "flag"), 64, "aec: bad flag\n"},
		{"validation", validationErr("no good"), 65, "aec: no good\n"},
		{"notfound", notFoundErr("rule %q", "x"), 66, "aec: rule \"x\"\n"},
		{"plain", errors.New("boom"), 1, "aec: boom\n"},
		{"silent", &cliError{code: 2}, 2, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withCommand(t, "fake", func([]string, io.Reader, io.Writer, io.Writer) error { return c.err })
			code, stderr := run(t, "", "fake")
			if code != c.code || stderr != c.want {
				t.Errorf("code=%d stderr=%q want %d %q", code, stderr, c.code, c.want)
			}
		})
	}
}

func TestCommandArgs(t *testing.T) {
	var got []string
	withCommand(t, "fake", func(args []string, _ io.Reader, _, _ io.Writer) error {
		got = args
		return nil
	})
	run(t, "", "fake", "a", "--b")
	if strings.Join(got, " ") != "a --b" {
		t.Errorf("args=%v", got)
	}
}

func TestHookRejectsArgs(t *testing.T) {
	useTempConfig(t)
	code, stderr := run(t, tryPHP, "hook", "edit", "extra")
	if code != 64 || !strings.HasPrefix(stderr, "aec: hook commands take no arguments") {
		t.Errorf("code=%d stderr=%q", code, stderr)
	}
}
