package aec

import (
	"os"
	"strings"
	"testing"
)

func readDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names, err
}

const mixedOverlay = `
disabled = ["no-throw"]

[[rules]]
name = "no-try-catch"
message = "changed"

[[rules]]
name = "my-rule"
files = ["go"]
pattern = '/panic\(/'
message = "no panics"
`

func TestRulesListDefaults(t *testing.T) {
	useTempConfig(t)
	code, stdout, stderr := runOut(t, "", "rules", "list")
	if code != 0 || !strings.Contains(stderr, "wrote starter overlay") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	defaults, _ := LoadDefaults()
	if n := strings.Count(stdout, "# default\n"); n != len(defaults) {
		t.Errorf("%d '# default' lines, want %d", n, len(defaults))
	}
	back, err := parseRules([]byte(stdout))
	if err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
	for i := range defaults {
		if !sameRule(defaults[i], back[i]) {
			t.Errorf("rule %q changed", defaults[i].Name)
		}
	}
}

func TestRulesListAnnotations(t *testing.T) {
	dir := useTempConfig(t)
	writeOverlay(t, dir, mixedOverlay)
	code, stdout, stderr := runOut(t, "", "rules", "list")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	for _, want := range []string{
		"# overridden\n[[rules]]\nname = \"no-try-catch\"\n",
		"# default, disabled\n[[rules]]\nname = \"no-throw\"\n",
		"# user\n[[rules]]\nname = \"my-rule\"\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in:\n%s", want, stdout)
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(stdout), "message = \"no panics\"") {
		t.Errorf("user rule should come last:\n%s", stdout)
	}
}

func TestRulesListOverriddenAndDisabled(t *testing.T) {
	dir := useTempConfig(t)
	writeOverlay(t, dir, "disabled = [\"no-throw\"]\n[[rules]]\nname = \"no-throw\"\nmessage = \"x\"\n")
	_, stdout, _ := runOut(t, "", "rules", "list")
	if !strings.Contains(stdout, "# overridden, disabled\n[[rules]]\nname = \"no-throw\"\n") {
		t.Errorf("annotation wrong:\n%s", stdout)
	}
}

func TestRulesShow(t *testing.T) {
	dir := useTempConfig(t)
	writeOverlay(t, dir, mixedOverlay)
	code, stdout, stderr := runOut(t, "", "rules", "show", "no-try-catch")
	if code != 0 || stderr != "" || !strings.HasPrefix(stdout, "[[rules]]\n") || strings.Count(stdout, "[[rules]]") != 1 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	back, err := parseRules([]byte(stdout))
	if err != nil {
		t.Fatal(err)
	}
	if !sameRule(back[0], defaultRule(t, "no-try-catch")) {
		t.Errorf("show should print the default, not the override:\n%s", stdout)
	}
}

func TestRulesShowErrors(t *testing.T) {
	dir := useTempConfig(t)
	code, stderr := run(t, "", "rules", "show", "nope")
	if code != 66 || stderr != "aec: no default rule named \"nope\"\n" {
		t.Errorf("code=%d stderr=%q", code, stderr)
	}
	code, _ = run(t, "", "rules", "show")
	if code != 64 {
		t.Errorf("no name: code=%d", code)
	}
	if entries, _ := readDir(dir); len(entries) != 0 {
		t.Errorf("rules show created files: %v", entries)
	}
}

func TestRulesDiffNoChanges(t *testing.T) {
	useTempConfig(t)
	code, stdout, _ := runOut(t, "", "rules", "diff")
	if code != 0 || stdout != "no changes from default\n" {
		t.Errorf("code=%d stdout=%q", code, stdout)
	}
}

func TestRulesDiffOverride(t *testing.T) {
	dir := useTempConfig(t)
	writeOverlay(t, dir, "[[rules]]\nname = \"no-throw\"\nmessage = \"mine\"\n")
	code, stdout, _ := runOut(t, "", "rules", "diff")
	want := "no-throw\n  message\n    default:  " + defaultRule(t, "no-throw").Message + "\n    override: mine\n\n"
	if code != 0 || stdout != want {
		t.Errorf("code=%d\ngot:\n%s\nwant:\n%s", code, stdout, want)
	}
}

func TestRulesDiffTwoKeys(t *testing.T) {
	dir := useTempConfig(t)
	writeOverlay(t, dir, "[[rules]]\nname = \"one-test-at-a-time\"\nmax_matches = 3\npattern = '/x/'\n")
	_, stdout, _ := runOut(t, "", "rules", "diff")
	p := strings.Index(stdout, "  pattern\n")
	m := strings.Index(stdout, "  max_matches\n")
	if p < 0 || m < 0 || p > m {
		t.Errorf("keys missing or out of order:\n%s", stdout)
	}
	if !strings.Contains(stdout, "    default:  1\n    override: 3\n") {
		t.Errorf("max_matches values wrong:\n%s", stdout)
	}
}

func TestRulesDiffListsOnly(t *testing.T) {
	dir := useTempConfig(t)
	writeOverlay(t, dir, "disabled = [\"no-throw\", \"no-spy\"]\n[[rules]]\nname = \"my-rule\"\nfiles = [\"go\"]\npattern = '/x/'\nmessage = \"m\"\n")
	_, stdout, _ := runOut(t, "", "rules", "diff")
	if stdout != "disabled: no-throw, no-spy\nuser rules: my-rule\n" {
		t.Errorf("stdout=%q", stdout)
	}
}

func TestRulesDiffStaleWarning(t *testing.T) {
	dir := useTempConfig(t)
	writeOverlay(t, dir, "disabled = [\"nope\"]\n")
	_, stdout, stderr := runOut(t, "", "rules", "diff")
	if stdout != "no changes from default\n" || !strings.Contains(stderr, "\"nope\" is not a default rule") {
		t.Errorf("stdout=%q stderr=%q", stdout, stderr)
	}
}
