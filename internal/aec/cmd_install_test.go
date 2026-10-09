package aec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// installEnv fakes the home directory, the binary location and the clock.
// It returns the home directory; the binary is reported at <home>/bin/aec.
func installEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	oldExe, oldNow := executablePath, now
	executablePath = func() (string, error) { return filepath.Join(home, "bin", "aec"), nil }
	now = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }
	t.Cleanup(func() { executablePath, now = oldExe, oldNow })
	return home
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return doc
}

// hookCommands lists the aec commands under PreToolUse,
// PostToolUseFailure, UserPromptSubmit, then PostToolUse.
func hookCommands(doc map[string]any) []string {
	var out []string
	hooks, _ := doc["hooks"].(map[string]any)
	for _, event := range []string{"PreToolUse", "PostToolUseFailure", "UserPromptSubmit", "PostToolUse"} {
		groups, _ := hooks[event].([]any)
		for _, g := range groups {
			for _, e := range g.(map[string]any)["hooks"].([]any) {
				if cmd := e.(map[string]any)["command"].(string); strings.Contains(cmd, "aec") {
					out = append(out, cmd)
				}
			}
		}
	}
	return out
}

var allAecHooks = []string{"~/bin/aec hook edit", "~/bin/aec hook bash", "~/bin/aec hook tool-fails", "~/bin/aec hook prompt", "~/bin/aec hook nudge"}

func backups(t *testing.T, path string) []string {
	t.Helper()
	m, _ := filepath.Glob(path + ".backup-*")
	return m
}

func TestInstallFresh(t *testing.T) {
	home := installEnv(t)
	path := filepath.Join(home, "x", "settings.json")
	code, stdout, stderr := runOut(t, "", "install", "--yes", "--settings="+path)
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	doc := readJSON(t, path)
	if got := hookCommands(doc); !reflect.DeepEqual(got, allAecHooks) {
		t.Errorf("commands=%v", got)
	}
	for _, event := range []string{"PostToolUseFailure", "UserPromptSubmit"} {
		group := doc["hooks"].(map[string]any)[event].([]any)[0].(map[string]any)
		if _, ok := group["matcher"]; ok {
			t.Errorf("%s group should have no matcher: %v", event, group)
		}
	}
	if b := backups(t, path); len(b) != 0 {
		t.Errorf("unexpected backups %v", b)
	}
	overlay := filepath.Join(home, ".config", "aec", "rules.toml")
	if _, err := os.Stat(overlay); err != nil {
		t.Errorf("starter overlay not written: %v", err)
	}
	for _, s := range []string{"(will be created)", "[add]    PreToolUse (matcher: Write|Edit) -> ~/bin/aec hook edit", "[add]    PostToolUseFailure -> ~/bin/aec hook tool-fails", "[add]    UserPromptSubmit -> ~/bin/aec hook prompt", "[add]    PostToolUse (matcher: Write|Edit|Read|Agent) -> ~/bin/aec hook nudge", "Wrote starter overlay to " + overlay, "Restart Claude Code"} {
		if !strings.Contains(stdout, s) {
			t.Errorf("missing %q in:\n%s", s, stdout)
		}
	}

	before, _ := os.ReadFile(path)
	code, stdout, _ = runOut(t, "", "install", "--yes", "--settings="+path)
	after, _ := os.ReadFile(path)
	if code != 0 || strings.Count(stdout, "[ok]") != 5 || !strings.Contains(stdout, "Nothing to do") {
		t.Errorf("second run: code=%d\n%s", code, stdout)
	}
	if string(before) != string(after) || len(backups(t, path)) != 0 {
		t.Errorf("second run changed the file or made a backup")
	}
}

func TestInstallUpdatesOldPath(t *testing.T) {
	home := installEnv(t)
	path := filepath.Join(home, "settings.json")
	old := `{"hooks":{"PreToolUse":[{"matcher":"Write|Edit","hooks":[{"type":"command","command":"/old/place/aec hook edit"}]}],` +
		`"PostToolUseFailure":[{"hooks":[{"type":"command","command":"/old/place/aec hook tool-fails"}]}]}}`
	os.WriteFile(path, []byte(old), 0o644)
	code, stdout, _ := runOut(t, "", "install", "-y", "--settings="+path)
	for _, s := range []string{
		"[update] PreToolUse: hook edit: path -> ~/bin/aec hook edit",
		"[update] PostToolUseFailure: hook tool-fails: path -> ~/bin/aec hook tool-fails\n",
	} {
		if code != 0 || !strings.Contains(stdout, s) {
			t.Errorf("code=%d, missing %q in:\n%s", code, s, stdout)
		}
	}
	doc := readJSON(t, path)
	if got := hookCommands(doc); !reflect.DeepEqual(got, allAecHooks) {
		t.Errorf("commands=%v", got)
	}
	if fail := doc["hooks"].(map[string]any)["PostToolUseFailure"].([]any); len(fail) != 1 || fail[0].(map[string]any)["matcher"] != nil {
		t.Errorf("tool-fails group should be updated in place with no matcher: %v", fail)
	}
	b := backups(t, path)
	if len(b) != 1 || filepath.Base(b[0]) != "settings.json.backup-20260102-030405" {
		t.Fatalf("backups=%v", b)
	}
	if data, _ := os.ReadFile(b[0]); string(data) != old {
		t.Errorf("backup content changed")
	}
}

func TestInstallLeavesOthersAlone(t *testing.T) {
	home := installEnv(t)
	path := filepath.Join(home, "settings.json")
	src := `{
  "mcpServers": {},
  "permissions": {"allow": ["Bash(ls:*)"]},
  "hooks": {
    "PreToolUse": [
      {"matcher": "Write|Edit", "hooks": [{"type": "command", "command": "~/code/agent-edit-checker/check.php"}]},
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "~/code/agent-edit-checker/tool-use.php"}]}
    ],
    "PostToolUse": [
      {"matcher": "Edit", "hooks": [{"type": "command", "command": "echo a && echo b"}]},
      {"matcher": "Write|Edit|Read|Agent", "hooks": [{"type": "command", "command": "~/code/agent-edit-checker/state-nudge.php"}]}
    ],
    "PostToolUseFailure": [{"hooks": [{"type": "command", "command": "~/code/agent-edit-checker/tool-fails.php"}]}],
    "UserPromptSubmit": [{"hooks": [{"type": "command", "command": "~/code/agent-edit-checker/prompt-context.php"}]}]
  }
}`
	os.WriteFile(path, []byte(src), 0o644)
	code, stdout, _ := runOut(t, "", "install", "-y", "--settings="+path)
	if code != 0 {
		t.Fatalf("code=%d\n%s", code, stdout)
	}
	for php, event := range map[string]string{"check.php": "PreToolUse", "tool-use.php": "PreToolUse", "tool-fails.php": "PostToolUseFailure", "prompt-context.php": "UserPromptSubmit", "state-nudge.php": "PostToolUse"} {
		if !strings.Contains(stdout, "[note]   "+event+": ~/code/agent-edit-checker/"+php+" is still installed") {
			t.Errorf("missing note for %s:\n%s", php, stdout)
		}
	}
	var want map[string]any
	json.Unmarshal([]byte(src), &want)
	got := readJSON(t, path)
	if !reflect.DeepEqual(got["mcpServers"], want["mcpServers"]) || !reflect.DeepEqual(got["permissions"], want["permissions"]) {
		t.Errorf("unrelated keys changed")
	}
	wantHooks := want["hooks"].(map[string]any)
	gotHooks := got["hooks"].(map[string]any)
	post := gotHooks["PostToolUse"].([]any)
	if len(post) != 3 || !reflect.DeepEqual(post[:2], wantHooks["PostToolUse"].([]any)) {
		t.Errorf("PostToolUse should keep its groups first: %v", post)
	}
	pre := gotHooks["PreToolUse"].([]any)
	if len(pre) != 4 || !reflect.DeepEqual(pre[:2], wantHooks["PreToolUse"].([]any)) {
		t.Errorf("PreToolUse should keep the PHP groups first: %v", pre)
	}
	fail := gotHooks["PostToolUseFailure"].([]any)
	if len(fail) != 2 || !reflect.DeepEqual(fail[:1], wantHooks["PostToolUseFailure"].([]any)) {
		t.Errorf("PostToolUseFailure should keep the PHP group first: %v", fail)
	}
	submit := gotHooks["UserPromptSubmit"].([]any)
	if len(submit) != 2 || !reflect.DeepEqual(submit[:1], wantHooks["UserPromptSubmit"].([]any)) {
		t.Errorf("UserPromptSubmit should keep the PHP group first: %v", submit)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "echo a && echo b") {
		t.Errorf("ampersand was escaped:\n%s", raw)
	}
}

func TestInstallDryRunAndAbort(t *testing.T) {
	home := installEnv(t)
	path := filepath.Join(home, "settings.json")
	overlay := filepath.Join(home, ".config", "aec", "rules.toml")

	code, stdout, _ := runOut(t, "", "install", "--dry-run", "--settings="+path)
	if code != 0 || !strings.Contains(stdout, "Dry run, no files were changed.") {
		t.Errorf("dry run: code=%d\n%s", code, stdout)
	}
	code, stdout, _ = runOut(t, "n\n", "install", "--settings="+path)
	if code != 0 || !strings.Contains(stdout, "Proceed? This will back up and update "+path+". [y/N] Aborted, nothing changed.") {
		t.Errorf("abort: code=%d\n%s", code, stdout)
	}
	for _, p := range []string{path, overlay} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s was written", p)
		}
	}
}

func TestInstallPromptYes(t *testing.T) {
	home := installEnv(t)
	path := filepath.Join(home, "settings.json")
	code, _, _ := runOut(t, "yes\n", "install", "--settings="+path)
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	if len(hookCommands(readJSON(t, path))) != 5 {
		t.Errorf("hooks not written")
	}
}

func TestInstallRefusesBadJSON(t *testing.T) {
	home := installEnv(t)
	path := filepath.Join(home, "settings.json")
	for _, src := range []string{`[]`, `{"hooks": []}`, `{"hooks": {"PreToolUse": {}}}`, `{"hooks": {"PostToolUseFailure": "x"}}`, `{nope`} {
		os.WriteFile(path, []byte(src), 0o644)
		code, stderr := run(t, "", "install", "-y", "--settings="+path)
		if code != 65 || !strings.Contains(stderr, path) || !strings.Contains(stderr, "leaving it untouched") {
			t.Errorf("%s: code=%d stderr=%q", src, code, stderr)
		}
		if data, _ := os.ReadFile(path); string(data) != src {
			t.Errorf("%s: file changed", src)
		}
	}
}

func TestInstallScopes(t *testing.T) {
	home := installEnv(t)
	cwd := t.TempDir()
	old := workDir
	workDir = func() (string, error) { return cwd, nil }
	t.Cleanup(func() { workDir = old })

	cases := map[string]string{
		"user":    filepath.Join(home, ".claude", "settings.json"),
		"project": filepath.Join(cwd, ".claude", "settings.json"),
		"local":   filepath.Join(cwd, ".claude", "settings.local.json"),
	}
	for scope, want := range cases {
		code, stdout, _ := runOut(t, "", "install", "-y", "--scope="+scope)
		if code != 0 || !strings.Contains(stdout, "settings: "+want+" (will be created)") {
			t.Errorf("%s: code=%d\n%s", scope, code, stdout)
		}
		if len(hookCommands(readJSON(t, want))) != 5 {
			t.Errorf("%s: hooks not written to %s", scope, want)
		}
	}
	if code, stderr := run(t, "", "install", "--scope=nope"); code != 64 || !strings.Contains(stderr, "--scope must be") {
		t.Errorf("bad scope: code=%d stderr=%q", code, stderr)
	}

	other := filepath.Join(home, "other.json")
	os.RemoveAll(filepath.Join(cwd, ".claude"))
	if code, _, _ := runOut(t, "", "install", "-y", "--scope=local", "--settings="+other); code != 0 {
		t.Fatalf("code=%d", code)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("--settings not honoured: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cwd, ".claude")); err == nil {
		t.Errorf("--scope=local wrote under .claude despite --settings")
	}
}

func TestInstallTildeAndAbsolute(t *testing.T) {
	home := installEnv(t)
	if got, _ := hookCommandPrefix(home); got != "~/bin/aec" {
		t.Errorf("under home: %q", got)
	}
	executablePath = func() (string, error) { return "/opt/tools/aec", nil }
	if got, _ := hookCommandPrefix(home); got != "/opt/tools/aec" {
		t.Errorf("outside home: %q", got)
	}
}

func TestIsAecHook(t *testing.T) {
	cases := map[string]bool{
		"~/bin/aec hook edit":                 true,
		"/usr/local/bin/aec hook edit":        true,
		`C:\tools\aec.exe hook edit`:          true,
		"aec hook edit":                       true,
		"~/bin/aec hook bash":                 false,
		"~/code/agent-edit-checker/check.php": false,
		"~/bin/notaec hook edit":              false,
	}
	for cmd, want := range cases {
		if got := isAecHook(cmd, "edit"); got != want {
			t.Errorf("isAecHook(%q) = %v", cmd, got)
		}
	}
	if !isAecHook("~/bin/aec hook tool-fails", "tool-fails") || isAecHook("~/bin/aec hook tool-fails", "edit") {
		t.Errorf("isAecHook mishandles tool-fails")
	}
}
