package aec

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
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

func TestLogDecision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool-use.log")
	LogDecision(path, false, "ls -la")
	LogDecision(path, true, "printf 'a\r\nb'")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "allowed | ls -la\ndenied | printf 'a\\r\\nb'\n"
	if string(got) != want {
		t.Errorf("log = %q, want %q", got, want)
	}
}

func TestLogDecisionUnwritable(t *testing.T) {
	LogDecision(filepath.Join(t.TempDir(), "missing", "dir", "log"), true, "x")
}
