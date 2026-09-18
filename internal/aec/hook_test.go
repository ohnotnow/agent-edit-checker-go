package aec

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// useTempConfig points configDir at a fresh directory and returns it.
func useTempConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old := configDir
	configDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { configDir = old })
	return dir
}

func writeOverlay(t *testing.T, dir, src string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "aec"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "aec", "rules.toml"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, stdin string, args ...string) (int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(args, strings.NewReader(stdin), &stdout, &stderr)
	if stdout.Len() != 0 {
		t.Errorf("stdout should be empty, got %q", stdout.String())
	}
	return code, stderr.String()
}

const tryPHP = `{"tool_input":{"file_path":"app/X.php","content":"try {"}}`

func TestHookEdit(t *testing.T) {
	useTempConfig(t)
	code, stderr := run(t, tryPHP, "hook", "edit")
	if code != 2 || !strings.Contains(stderr, "\u274c Blocked: Don't add try/catch blocks") {
		t.Errorf("code=%d stderr=%q", code, stderr)
	}
	code, stderr = run(t, `{"tool_input":{"file_path":"x.py","content":"try {"}}`, "hook", "edit")
	if code != 0 || stderr != "" {
		t.Errorf("py: code=%d stderr=%q", code, stderr)
	}
}

func TestHookEditContentSelection(t *testing.T) {
	useTempConfig(t)
	code, _ := run(t, `{"tool_input":{"file_path":"x.php","old_string":"a","new_string":"try {"}}`, "hook", "edit")
	if code != 2 {
		t.Errorf("new_string should be checked, code=%d", code)
	}
	code, _ = run(t, `{"tool_input":{"file_path":"x.php","content":"ok","new_string":"try {"}}`, "hook", "edit")
	if code != 0 {
		t.Errorf("content should win over new_string, code=%d", code)
	}
}

func TestHookBash(t *testing.T) {
	dir := useTempConfig(t)
	code, stderr := run(t, `{"tool_input":{"command":"npm install foo"}}`, "hook", "bash")
	if code != 2 || !strings.Contains(stderr, "Never run npm install") {
		t.Errorf("code=%d stderr=%q", code, stderr)
	}
	code, stderr = run(t, `{"tool_input":{"command":"npm run build"}}`, "hook", "bash")
	if code != 0 || stderr != "" {
		t.Errorf("code=%d stderr=%q", code, stderr)
	}
	log, err := os.ReadFile(filepath.Join(dir, "aec", "tool-use.log"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "denied | npm install foo\nallowed | npm run build\n"; string(log) != want {
		t.Errorf("log=%q want %q", log, want)
	}
}

func TestHookInvalidJSON(t *testing.T) {
	useTempConfig(t)
	for _, kind := range []string{"edit", "bash"} {
		code, stderr := run(t, "not json", "hook", kind)
		if code != 0 || stderr != "" {
			t.Errorf("%s: code=%d stderr=%q", kind, code, stderr)
		}
	}
}

func TestHookBrokenOverlay(t *testing.T) {
	dir := useTempConfig(t)
	writeOverlay(t, dir, "disabled = [\n")
	code, stderr := run(t, tryPHP, "hook", "edit")
	if code != 2 || !strings.Contains(stderr, "aec: overlay ignored:") || !strings.Contains(stderr, "Blocked:") {
		t.Errorf("code=%d stderr=%q", code, stderr)
	}
}

func TestHookDisabledOverlay(t *testing.T) {
	dir := useTempConfig(t)
	writeOverlay(t, dir, `disabled = ["no-try-catch"]`)
	code, stderr := run(t, tryPHP, "hook", "edit")
	if code != 0 || stderr != "" {
		t.Errorf("code=%d stderr=%q", code, stderr)
	}
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"nonsense"}, {"hook"}, {"hook", "nope"}, {"rules"}, {"rules", "nope"}} {
		code, stderr := run(t, "", args...)
		if code != 64 || !strings.HasPrefix(stderr, "usage:") {
			t.Errorf("%v: code=%d stderr=%q", args, code, stderr)
		}
	}
}
