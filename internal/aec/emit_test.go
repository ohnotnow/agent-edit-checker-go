package aec

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

func emitOne(t *testing.T, r Rule) string {
	t.Helper()
	var b bytes.Buffer
	if err := emitRule(&b, r); err != nil {
		t.Fatalf("emitRule(%s): %v", r.Name, err)
	}
	return b.String()
}

func sameRule(a, b Rule) bool {
	if a.MaxMatches == nil != (b.MaxMatches == nil) {
		return false
	}
	if a.MaxMatches != nil && *a.MaxMatches != *b.MaxMatches {
		return false
	}
	return a.Name == b.Name && a.Pattern == b.Pattern && a.Message == b.Message &&
		a.Command == b.Command && a.Type == b.Type && slices.Equal(a.Files, b.Files)
}

func TestEmitRoundTripsDefaults(t *testing.T) {
	defaults, err := LoadDefaults()
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	for _, r := range defaults {
		if err := emitRule(&b, r); err != nil {
			t.Fatal(err)
		}
	}
	back, err := parseRules(b.Bytes())
	if err != nil {
		t.Fatalf("parse emitted TOML: %v\n%s", err, b.String())
	}
	if len(back) != len(defaults) {
		t.Fatalf("got %d rules back, want %d", len(back), len(defaults))
	}
	for i := range defaults {
		if !sameRule(defaults[i], back[i]) {
			t.Errorf("rule %q changed:\n%+v\n%+v", defaults[i].Name, defaults[i], back[i])
		}
	}
}

func TestEmitKeyOrderAndDelimiters(t *testing.T) {
	one := 1
	out := emitOne(t, Rule{
		Name: "a", Files: []string{"php", ".blade.php"}, Command: `/x/`, Type: "require",
		Pattern: `/it's\s/`, MaxMatches: &one, Message: `say "hi" \ there`,
	})
	want := "[[rules]]\n" +
		"name = \"a\"\n" +
		"files = [\"php\", \".blade.php\"]\n" +
		"command = '/x/'\n" +
		"type = \"require\"\n" +
		"pattern = '''/it's\\s/'''\n" +
		"max_matches = 1\n" +
		"message = 'say \"hi\" \\ there'\n\n"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestEmitOmitsUnsetKeys(t *testing.T) {
	out := emitOne(t, Rule{Name: "b", Command: `/c/`, Pattern: `/p/`, Message: "m"})
	for _, absent := range []string{"files", "type", "max_matches"} {
		if strings.Contains(out, absent) {
			t.Errorf("%q should be omitted:\n%s", absent, out)
		}
	}
	if !strings.Contains(out, "command = '/c/'\n") {
		t.Errorf("command missing:\n%s", out)
	}
}

func TestEmitMessageEscapes(t *testing.T) {
	cases := []string{`plain`, `back\slash`, `quote " and it's`, `it's`, `ends with "`}
	for _, msg := range cases {
		out := emitOne(t, Rule{Name: "m", Files: []string{"php"}, Pattern: `/x/`, Message: msg})
		back, err := parseRules([]byte(out))
		if err != nil {
			t.Errorf("%q: parse: %v\n%s", msg, err, out)
			continue
		}
		if back[0].Message != msg {
			t.Errorf("%q came back as %q", msg, back[0].Message)
		}
	}
}

func TestEmitErrors(t *testing.T) {
	cases := []struct {
		label string
		rule  Rule
		want  string
	}{
		{"newline in message", Rule{Name: "n", Files: []string{"php"}, Pattern: `/x/`, Message: "a\nb"}, `rule "n": message`},
		{"pattern ends with quote", Rule{Name: "p", Files: []string{"php"}, Pattern: `/x'/'`, Message: "m"}, `rule "p": pattern`},
		{"triple quote in command", Rule{Name: "c", Command: `/'''/`, Pattern: `/x/`, Message: "m"}, `rule "c": command`},
	}
	for _, c := range cases {
		var b bytes.Buffer
		err := emitRule(&b, c.rule)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err=%v want %q", c.label, err, c.want)
		}
	}
}
