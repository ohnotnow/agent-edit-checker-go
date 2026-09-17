package aec

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestOverlayPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/x/cfg")
	got, err := OverlayPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/x/cfg", "aec", "rules.toml"); got != want {
		t.Errorf("OverlayPath() = %q, want %q", got, want)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/x")
	got, err = OverlayPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/home/x", ".config", "aec", "rules.toml"); got != want {
		t.Errorf("OverlayPath() = %q, want %q", got, want)
	}
}

func TestLoadOverlayMissing(t *testing.T) {
	ov, err := LoadOverlay(filepath.Join(t.TempDir(), "rules.toml"))
	if ov != nil || err != nil {
		t.Errorf("got %v, %v; want nil, nil", ov, err)
	}
}

func TestLoadOverlayInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.toml")
	os.WriteFile(path, []byte("disabled = [\n"), 0o644)
	_, err := LoadOverlay(path)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("error should name the file, got %v", err)
	}
}

func mergeTOML(t *testing.T, src string) (Merged, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rules.toml")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	ov, err := LoadOverlay(path)
	if err != nil {
		return Merged{}, err
	}
	defaults, err := LoadDefaults()
	if err != nil {
		t.Fatal(err)
	}
	return Merge(defaults, ov)
}

func mustMerge(t *testing.T, src string) Merged {
	t.Helper()
	m, err := mergeTOML(t, src)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func ruleByName(t *testing.T, rules []Rule, name string) Rule {
	t.Helper()
	for _, r := range rules {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no rule %q", name)
	return Rule{}
}

func TestMergeNoOverlay(t *testing.T) {
	defaults, _ := LoadDefaults()
	m, err := Merge(defaults, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Rules) != len(defaults) || len(m.Stale) != 0 {
		t.Fatalf("got %d rules, stale %v", len(m.Rules), m.Stale)
	}
	for _, r := range m.Rules {
		if !r.Enabled || r.Origin != OriginDefault {
			t.Errorf("%s: enabled=%v origin=%v", r.Name, r.Enabled, r.Origin)
		}
	}
}

func TestMergeDisabled(t *testing.T) {
	m := mustMerge(t, `disabled = ["no-try-catch"]`)
	for _, r := range m.Rules {
		if r.Enabled == (r.Name == "no-try-catch") {
			t.Errorf("%s: enabled=%v", r.Name, r.Enabled)
		}
	}
	if got := CheckContent(m.Rules, "php", "try {", ""); len(got) != 0 {
		t.Errorf("disabled rule still fired: %v", got)
	}
}

func TestMergeOverridePattern(t *testing.T) {
	m := mustMerge(t, "[[rules]]\nname = \"no-try-catch\"\npattern = '/catch/'\n")
	r := ruleByName(t, m.Rules, "no-try-catch")
	if r.Origin != OriginOverridden {
		t.Errorf("origin = %v", r.Origin)
	}
	if !strings.HasPrefix(r.Message, "Don't add try/catch") {
		t.Errorf("message changed: %q", r.Message)
	}
	if matches(r.re, "try {") || !matches(r.re, "catch") {
		t.Error("compiled pattern not replaced")
	}
}

func TestMergeOverrideMessageOnly(t *testing.T) {
	m := mustMerge(t, "[[rules]]\nname = \"no-try-catch\"\nmessage = \"nope\"\n")
	r := ruleByName(t, m.Rules, "no-try-catch")
	if r.Message != "nope" || r.Origin != OriginOverridden || !matches(r.re, "try {") {
		t.Errorf("got %+v", r)
	}
}

func TestMergeNameOnlyBlock(t *testing.T) {
	m := mustMerge(t, "[[rules]]\nname = \"no-try-catch\"\n")
	if r := ruleByName(t, m.Rules, "no-try-catch"); r.Origin != OriginDefault {
		t.Errorf("origin = %v", r.Origin)
	}
}

func TestMergeEnabledKeyIgnored(t *testing.T) {
	m := mustMerge(t, "disabled = [\"no-try-catch\"]\n[[rules]]\nname = \"no-try-catch\"\nenabled = true\n")
	if r := ruleByName(t, m.Rules, "no-try-catch"); r.Enabled || r.Origin != OriginDefault {
		t.Errorf("got enabled=%v origin=%v", r.Enabled, r.Origin)
	}
}

func TestMergeUserRules(t *testing.T) {
	m := mustMerge(t, `
disabled = ["second", "ghost", "half"]

[[rules]]
name = "half"
message = "only a message"

[[rules]]
name = "first"
files = ["py"]
pattern = '/print\(/'
message = "no print"

[[rules]]
name = "second"
command = '/./s'
pattern = '/x/'
message = "x"
`)
	n := len(m.Rules)
	first, second := m.Rules[n-2], m.Rules[n-1]
	if first.Name != "first" || second.Name != "second" {
		t.Fatalf("user rules should be last in overlay order, got %s, %s", first.Name, second.Name)
	}
	if first.Origin != OriginUser || !first.Enabled {
		t.Errorf("first: origin=%v enabled=%v", first.Origin, first.Enabled)
	}
	if second.Enabled {
		t.Error("disabled should switch off a user rule")
	}
	if want := []string{"half", "ghost"}; !slices.Equal(m.Stale, want) {
		t.Errorf("stale = %v, want %v", m.Stale, want)
	}
	if got := CheckContent(m.Rules, "py", "print('x')", ""); !slices.Equal(got, []string{"no print"}) {
		t.Errorf("user rule did not fire: %v", got)
	}
}

func TestMergeErrors(t *testing.T) {
	cases := []struct{ label, src, want string }{
		{"bad regex", "[[rules]]\nname = \"no-try-catch\"\npattern = '/(/'\n", `rule "no-try-catch"`},
		{"unknown flag", "[[rules]]\nname = \"no-try-catch\"\npattern = '/x/z'\n", `'z'`},
		{"files emptied", "[[rules]]\nname = \"no-try-catch\"\nfiles = []\n", `rule "no-try-catch": needs at least one`},
		{"bad type", "[[rules]]\nname = \"npm-install\"\ntype = \"maybe\"\n", `rule "npm-install": type "maybe"`},
		{"duplicate", "[[rules]]\nname = \"a\"\n[[rules]]\nname = \"a\"\n", `rule "a": duplicate`},
		{"nameless", "[[rules]]\nname = \"a\"\n[[rules]]\nmessage = \"m\"\n", "rules[1] has no name"},
		{"bad user rule", "[[rules]]\nname = \"mine\"\nfiles = ['php']\npattern = '/(/'\nmessage = 'm'\n", `rule "mine"`},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			_, err := mergeTOML(t, c.src)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not contain %q", err, c.want)
			}
		})
	}
}
