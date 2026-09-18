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
