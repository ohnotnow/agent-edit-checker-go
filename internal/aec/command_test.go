package aec

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

const commandRules = `
[[rules]]
name = "forbid-update"
command = '/\bcomposer\s/'
type = "forbid"
pattern = '/\bupdate\b/'
message = "no update"

[[rules]]
name = "require-compact"
command = '/\bpest\b/'
type = "require"
pattern = '/--compact/'
message = "need compact"

[[rules]]
name = "files-only"
files = ["*"]
pattern = '/./'
message = "files only"

[[rules]]
name = "everything"
command = '/./s'
pattern = '/dash/'
message = "dash"
`

func TestCheckCommand(t *testing.T) {
	rules := testRules(t, commandRules)
	cases := []struct {
		command string
		want    []string
	}{
		{"composer update", []string{"no update"}},
		{"composer install", nil},
		{"npm update", nil},
		{"pest", []string{"need compact"}},
		{"pest --compact", nil},
		{"ls", nil},
		{"echo dash", []string{"dash"}},
		{"echo\ndash", []string{"dash"}},
		{"composer update\npest\ndash", []string{"no update", "need compact", "dash"}},
	}
	for _, c := range cases {
		if got := CheckCommand(rules, c.command); !slices.Equal(got, c.want) {
			t.Errorf("CheckCommand(%q) = %v, want %v", c.command, got, c.want)
		}
	}
}

func TestCheckCommandSkipsDisabled(t *testing.T) {
	rules := testRules(t, commandRules)
	rules[0].Enabled = false
	if got := CheckCommand(rules, "composer update"); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

// useFixedClock pins now to 2026-10-09T19:30:12+01:00 for the test.
func useFixedClock(t *testing.T) {
	t.Helper()
	orig := now
	t.Cleanup(func() { now = orig })
	now = func() time.Time { return time.Date(2026, 10, 9, 19, 30, 12, 0, time.FixedZone("BST", 3600)) }
}

func TestLogDecision(t *testing.T) {
	useFixedClock(t)
	path := filepath.Join(t.TempDir(), "tool-use.log")
	LogDecision(path, false, "ls -la")
	LogDecision(path, true, "printf 'a\r\nb'")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "2026-10-09T19:30:12+01:00 | allowed | ls -la\n" +
		"2026-10-09T19:30:12+01:00 | denied | printf 'a\\r\\nb'\n"
	if string(got) != want {
		t.Errorf("log = %q, want %q", got, want)
	}
}

func TestLogDecisionUnwritable(t *testing.T) {
	LogDecision(filepath.Join(t.TempDir(), "missing", "dir", "log"), true, "x")
}
