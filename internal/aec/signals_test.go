package aec

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func signalStates(sigs []Signal) []string {
	var out []string
	for _, s := range sigs {
		state := "on"
		if !s.Enabled {
			state = "off"
		}
		out = append(out, s.Name+"="+state)
	}
	return out
}

func TestMergeSignals(t *testing.T) {
	all := []string{"state-load=on", "confidence-check=on", "test-quality=on"}
	rules, _ := LoadDefaults()
	m, err := Merge(rules, nil)
	if err != nil || !slices.Equal(signalStates(m.Signals), all) {
		t.Errorf("nil overlay: %v %v", err, signalStates(m.Signals))
	}
	if got := signalStates(mustMerge(t, "").Signals); !slices.Equal(got, all) {
		t.Errorf("empty overlay: %v", got)
	}
	m = mustMerge(t, `disabled = ["confidence-check", "no-throw"]`)
	if got := signalStates(m.Signals); !slices.Equal(got, []string{"state-load=on", "confidence-check=off", "test-quality=on"}) {
		t.Errorf("got %v", got)
	}
	if len(m.Stale) != 0 {
		t.Errorf("signal name reported stale: %v", m.Stale)
	}
}

func TestReservedSignalNames(t *testing.T) {
	_, err := parseRules([]byte("[[rules]]\nname='test-quality'\nfiles=['php']\npattern='/x/'\nmessage='m'\n"))
	if err == nil || !strings.Contains(err.Error(), `rule "test-quality": name is reserved for a nudge signal`) {
		t.Errorf("default: err=%v", err)
	}
	for _, src := range []string{
		"[[rules]]\nname = \"state-load\"\nmessage = \"only a message\"\n",
		"[[rules]]\nname = \"state-load\"\nfiles = ['php']\npattern = '/x/'\nmessage = 'm'\n",
	} {
		_, err := mergeTOML(t, src)
		if err == nil || !strings.Contains(err.Error(), `rule "state-load": name is reserved for a nudge signal`) {
			t.Errorf("%q: err=%v", src, err)
		}
	}
}

func TestRulesListShowsSignals(t *testing.T) {
	dir := useTempConfig(t)
	writeOverlay(t, dir, `disabled = ["test-quality"]`)
	_, stdout, _ := runOut(t, "", "rules", "list")
	for _, s := range []string{"# signal state-load: enabled\n", "# signal confidence-check: enabled\n", "# signal test-quality: disabled\n"} {
		if !strings.Contains(stdout, s) {
			t.Errorf("missing %q", s)
		}
	}
}

func TestRulesDiffShowsDisabledSignal(t *testing.T) {
	dir := useTempConfig(t)
	writeOverlay(t, dir, `disabled = ["confidence-check"]`)
	_, stdout, _ := runOut(t, "", "rules", "diff")
	if stdout != "disabled: confidence-check\n" {
		t.Errorf("got %q", stdout)
	}
}

func TestTuiTogglesSignals(t *testing.T) {
	dir := useTempConfig(t)
	path := filepath.Join(dir, "aec", "rules.toml")
	merged := mustMerge(t, "")
	m := tuiModel{rules: merged.Rules, signals: merged.Signals, path: path, width: 100, height: 100}
	if !strings.Contains(m.View(), "[x] test-quality") {
		t.Fatalf("view lacks signal rows:\n%s", m.View())
	}
	for range len(merged.Rules) + 2 {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		m = next.(tuiModel)
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = next.(tuiModel)
	ov, err := LoadOverlay(path)
	if err != nil || !slices.Equal(ov.Disabled, []string{"test-quality"}) || m.signals[2].Enabled {
		t.Fatalf("toggle: %v %v enabled=%v", ov, err, m.signals[2].Enabled)
	}
	if row := m.row(len(m.rules) + 2); !strings.Contains(row, "[ ] test-quality") || !strings.Contains(row, "test review") {
		t.Errorf("row = %q", row)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = next.(tuiModel)
	if m.cursor != len(m.rules)+2 {
		t.Errorf("cursor moved past the last row: %d", m.cursor)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = next.(tuiModel)
	ov, _ = LoadOverlay(path)
	if len(ov.Disabled) != 0 || !m.signals[2].Enabled {
		t.Errorf("toggle back: %v", ov.Disabled)
	}
}
