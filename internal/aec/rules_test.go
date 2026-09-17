package aec

import (
	"strings"
	"testing"

	"github.com/dlclark/regexp2"
)

func countMatches(t *testing.T, re *regexp2.Regexp, s string) int {
	t.Helper()
	n := 0
	m, err := re.FindStringMatch(s)
	for ; m != nil && err == nil; m, err = re.FindNextMatch(m) {
		n++
	}
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	return n
}

func TestLoadDefaults(t *testing.T) {
	rules, err := LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}
	if len(rules) == 0 {
		t.Fatal("no rules loaded")
	}
	if rules[0].Name != "one-test-at-a-time" {
		t.Errorf("first rule = %q, want file order kept", rules[0].Name)
	}
	for _, r := range rules {
		if r.re == nil {
			t.Errorf("rule %q: pattern not compiled", r.Name)
		}
		if (r.Command != "") != (r.commandRe != nil) {
			t.Errorf("rule %q: command compiled state does not match command field", r.Name)
		}
	}
}

const validRule = `
[[rules]]
name = "ok"
files = ["php"]
pattern = '/x/'
message = "m"
`

func TestParseRulesValid(t *testing.T) {
	rules, err := parseRules([]byte(validRule))
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].MaxMatches != nil || rules[0].Type != "" {
		t.Errorf("unexpected rule: %+v", rules[0])
	}
}

func TestParseRulesValidation(t *testing.T) {
	cases := []struct {
		label, toml, want string
	}{
		{"empty name", "[[rules]]\nfiles=['php']\npattern='/x/'\nmessage='m'\n", "name is empty"},
		{"bad name", "[[rules]]\nname='Bad_Name'\nfiles=['php']\npattern='/x/'\nmessage='m'\n", `rule "Bad_Name": name must be`},
		{"duplicate name", validRule + validRule, `rule "ok": duplicate name`},
		{"empty pattern", "[[rules]]\nname='p'\nfiles=['php']\nmessage='m'\n", `rule "p": pattern is empty`},
		{"empty message", "[[rules]]\nname='p'\nfiles=['php']\npattern='/x/'\n", `rule "p": message is empty`},
		{"no files or command", "[[rules]]\nname='p'\npattern='/x/'\nmessage='m'\n", `rule "p": needs at least one of files or command`},
		{"bad type", "[[rules]]\nname='p'\ncommand='/x/'\ntype='maybe'\npattern='/x/'\nmessage='m'\n", `rule "p": type "maybe" must be`},
		{"require without command", "[[rules]]\nname='p'\nfiles=['php']\ntype='require'\npattern='/x/'\nmessage='m'\n", `rule "p": type "require" needs command`},
		{"max_matches without files", "[[rules]]\nname='p'\ncommand='/x/'\nmax_matches=1\npattern='/x/'\nmessage='m'\n", `rule "p": max_matches needs files`},
		{"bad pattern", "[[rules]]\nname='p'\nfiles=['php']\npattern='/(/'\nmessage='m'\n", `rule "p": pattern "/(/"`},
		{"bad command", "[[rules]]\nname='p'\ncommand='no-delims'\npattern='/x/'\nmessage='m'\n", `rule "p": command pattern "no-delims"`},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			_, err := parseRules([]byte(c.toml))
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not contain %q", err.Error(), c.want)
			}
		})
	}
}

func TestParseRulesAcceptsTypes(t *testing.T) {
	for _, typ := range []string{"forbid", "require"} {
		src := "[[rules]]\nname='p'\ncommand='/x/'\ntype='" + typ + "'\npattern='/x/'\nmessage='m'\n"
		if _, err := parseRules([]byte(src)); err != nil {
			t.Errorf("type %q: %v", typ, err)
		}
	}
}

var defaultRuleNames = []string{
	"one-test-at-a-time", "no-try-catch", "no-throw", "no-mockery", "no-spy",
	"no-raw-db", "no-raw-methods", "no-database-assertions", "no-select",
	"no-php-blocks", "no-flux-field", "no-float-columns", "no-decimal-columns",
	"pest-compact", "composer-install", "npm-install", "pypi-install",
	"env-access", "git-commit-attribution", "gh-pr-attribution",
	"no-em-dash", "no-disguised-dash",
}

func TestDefaultRuleNamesAndOrder(t *testing.T) {
	rules, err := LoadDefaults()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != len(defaultRuleNames) {
		t.Fatalf("got %d rules, want %d", len(rules), len(defaultRuleNames))
	}
	for i, want := range defaultRuleNames {
		if rules[i].Name != want {
			t.Errorf("rule %d = %q, want %q", i, rules[i].Name, want)
		}
	}
	for _, name := range []string{"no-em-dash", "no-disguised-dash"} {
		r := defaultRule(t, name)
		if len(r.Files) != 1 || r.Files[0] != "*" || r.Command != "/./s" {
			t.Errorf("%s: files=%v command=%q, want [*] and /./s", name, r.Files, r.Command)
		}
	}
}

func defaultRule(t *testing.T, name string) Rule {
	t.Helper()
	rules, err := LoadDefaults()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rules {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no default rule %q", name)
	return Rule{}
}

func checkRule(t *testing.T, name string, matches, misses []string) {
	t.Helper()
	r := defaultRule(t, name)
	for _, s := range matches {
		if ok, _ := r.re.MatchString(s); !ok {
			t.Errorf("%s should match %q", name, s)
		}
	}
	for _, s := range misses {
		if ok, _ := r.re.MatchString(s); ok {
			t.Errorf("%s should not match %q", name, s)
		}
	}
}

func TestDefaultRuleMatches(t *testing.T) {
	dash := func(cp rune) string { return "a" + string(cp) + "b" }
	checkRule(t, "no-em-dash",
		[]string{dash(0x2014), dash(0x2013), dash(0x2012), dash(0x2015), dash(0xFE58)},
		[]string{"a-b", dash(0x2212)})
	checkRule(t, "no-disguised-dash",
		[]string{"&mdash;", "&#8212;", "&#x2014;", "\\u2014", "\\x{2014}", "\\N{EM DASH}",
			"\\xe2\\x80\\x94", "\\342\\200\\224", "chr(8212)", "mb_chr(0x2014)", "String.fromCharCode(8212)"},
		[]string{"&mdashes", "chr(82120)", dash(0x2014)})
	checkRule(t, "no-raw-db", []string{"DB::table("}, []string{"DB::transaction("})
	checkRule(t, "env-access",
		[]string{"cat .env", "export FOO=bar", "env | grep X", "; printenv"},
		[]string{"envsubst < tpl", "echo environment", `--description "export the data"`})
	checkRule(t, "pypi-install",
		[]string{"uv add requests", "pip install x"},
		[]string{"uv run pytest", "python scripts/add_occasion.py"})
	checkRule(t, "composer-install",
		[]string{"composer update"},
		[]string{"composer updated", "composer install"})
}
