package aec

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestDisabledNames(t *testing.T) {
	rules := []Rule{
		{Name: "a", Enabled: false},
		{Name: "b", Enabled: true},
		{Name: "c", Enabled: false},
	}
	if got := disabledNames(rules); !slices.Equal(got, []string{"a", "c"}) {
		t.Errorf("got %v", got)
	}
	if got := disabledNames(nil); got != nil {
		t.Errorf("nil rules should give nil, got %v", got)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello world", 8); got != "hello..." {
		t.Errorf("got %q", got)
	}
	if got := truncate("short", 8); got != "short" {
		t.Errorf("got %q", got)
	}
	if got := truncate("x", 0); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestTuiToggleWritesOverlay(t *testing.T) {
	dir := useTempConfig(t)
	path := filepath.Join(dir, "aec", "rules.toml")
	defaults, _ := LoadDefaults()
	m := tuiModel{rules: defaults, path: path, width: 80, height: 24}
	m.cursor = 1

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = next.(tuiModel)
	if m.rules[1].Enabled || m.status != "saved" {
		t.Errorf("toggle: enabled=%v status=%q", m.rules[1].Enabled, m.status)
	}
	ov, err := LoadOverlay(path)
	if err != nil || !slices.Equal(ov.Disabled, []string{defaults[1].Name}) {
		t.Errorf("overlay: %v %v", ov, err)
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(tuiModel)
	ov, _ = LoadOverlay(path)
	if !m.rules[1].Enabled || len(ov.Disabled) != 0 {
		t.Errorf("toggle back: enabled=%v disabled=%v", m.rules[1].Enabled, ov.Disabled)
	}
	if !strings.Contains(m.View(), "[x] "+defaults[1].Name) {
		t.Errorf("view missing the row:\n%s", m.View())
	}
}

func TestTuiWriteFailureRevertsToggle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.toml")
	os.WriteFile(path, []byte("disabled = [\n"), 0o644)
	defaults, _ := LoadDefaults()
	m := tuiModel{rules: defaults, path: path}
	m = m.toggle()
	if !m.rules[0].Enabled || !m.failed || !strings.HasPrefix(m.status, "write failed: ") {
		t.Errorf("enabled=%v failed=%v status=%q", m.rules[0].Enabled, m.failed, m.status)
	}
}

func TestTuiRowShowsFirstLineOfMessage(t *testing.T) {
	m := tuiModel{rules: []Rule{{Name: "ask", Enabled: true, Message: "first line\nsecond line"}}, width: 80}
	row := m.row(0)
	if strings.Contains(row, "\n") || strings.Contains(row, "second") || !strings.Contains(row, "first line") {
		t.Errorf("row = %q", row)
	}
}

const tuiTestOverlay = `
[[rules]]
name = "no-dd"
files = ["php"]
pattern = '/\bdd\(/'
message = "No dd()."

[nudge]
dirty_threshold = 3
`

func TestTuiGroupsRowsUnderHeadings(t *testing.T) {
	m := newTuiModel(mustMerge(t, tuiTestOverlay), "", true)
	m.height = 100
	view := m.View()
	order := []string{"Blocking rules", "no-mockery", "no-dd", "Prompt rules", "question-pause", "Nudges", "state-load", "test-quality"}
	last := -1
	for _, want := range order {
		at := strings.Index(view, want)
		if at <= last {
			t.Fatalf("%q missing or out of order in:\n%s", want, view)
		}
		last = at
	}
}

func TestTuiNudgeDetails(t *testing.T) {
	merged := mustMerge(t, tuiTestOverlay)
	m := newTuiModel(merged, "", true)
	m.height, m.width = 100, 200
	view := m.View()
	for _, want := range []string{"at 3 files; by model: ", "opus 7", "at 12 edits in a row", "every 8 test files, reviewed by test-quality-checker"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "agent not found") {
		t.Errorf("warned about an installed agent:\n%s", view)
	}

	m = newTuiModel(merged, "", false)
	m.height, m.width = 100, 200
	if !strings.Contains(m.View(), "agent not found") {
		t.Errorf("no warning for a missing agent:\n%s", m.View())
	}
}

func TestTuiScrollKeepsNudgeDetailInView(t *testing.T) {
	m := newTuiModel(mustMerge(t, ""), "", true)
	m.height, m.width = 10, 120
	m.cursor = len(m.rules) + len(m.signals) - 1
	if view := m.View(); !strings.Contains(view, "every 8 test files") {
		t.Errorf("last row's detail scrolled off:\n%s", view)
	}
}

func TestTuiMessagesLineUpPastOriginTags(t *testing.T) {
	merged := mustMerge(t, tuiTestOverlay+`
[[rules]]
name = "grounded-recommendation"
message = "Read the code first."
`)
	m := newTuiModel(merged, "", true)
	m.width, m.cursor = 200, -1
	column := func(name, msg string) int {
		t.Helper()
		for i := range len(m.rules) + len(m.signals) {
			if row := m.row(i); strings.Contains(row, name) {
				return strings.Index(row, msg)
			}
		}
		t.Fatalf("no row for %s", name)
		return 0
	}
	want := column("no-mockery", "Don't use Mockery")
	if got := column("no-dd (user)", "No dd()."); got != want {
		t.Errorf("user rule message at column %d, want %d", got, want)
	}
	if got := column("grounded-recommendation (overridden)", "Read the code first."); got != want {
		t.Errorf("overridden rule message at column %d, want %d", got, want)
	}
}

func TestTuiViewShowsRuleAsToml(t *testing.T) {
	defaults, _ := LoadDefaults()
	m := newTuiModel(Merged{Rules: defaults}, "", false)
	if strings.Contains(m.View(), "pattern = ") {
		t.Fatal("view open before v was pressed")
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	m = next.(tuiModel)
	if !strings.Contains(m.View(), m.rules[0].Pattern) || !strings.Contains(m.View(), "[[rules]]") {
		t.Errorf("pattern not shown:\n%s", m.View())
	}
}

func TestTuiPublish(t *testing.T) {
	dir := useTempConfig(t)
	path := filepath.Join(dir, "aec", "rules.toml")
	defaults, _ := LoadDefaults()
	m := newTuiModel(Merged{Rules: defaults, Signals: mergeSignals(nil)}, path, true)
	press := func() {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
		m = next.(tuiModel)
	}

	press()
	name := m.rules[0].Name
	if m.failed || m.rules[0].Origin != OriginOverridden || !strings.HasSuffix(m.status, " in "+path) {
		t.Fatalf("publish: status=%q origin=%v", m.status, m.rules[0].Origin)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), "name = \""+name+"\"") {
		t.Errorf("rule not in overlay:\n%s", data)
	}

	press()
	if !m.failed || !strings.Contains(m.status, "already in your overlay") {
		t.Errorf("second publish: failed=%v status=%q", m.failed, m.status)
	}

	m.cursor = len(m.rules)
	press()
	if !m.failed || !strings.Contains(m.status, "only rules") {
		t.Errorf("nudge publish: failed=%v status=%q", m.failed, m.status)
	}
}

func TestTuiShowsOverlayPathUnderHelp(t *testing.T) {
	home := t.TempDir()
	old := userHomeDir
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = old })
	m := newTuiModel(Merged{}, filepath.Join(home, ".config", "aec", "rules.toml"), true)
	lines := strings.Split(m.View(), "\n")
	if !strings.Contains(lines[1], "overlay: ~/.config/aec/rules.toml") {
		t.Errorf("second line: %q", lines[1])
	}
}
