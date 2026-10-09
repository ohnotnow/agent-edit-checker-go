package aec

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// nudgeEnv points the config dir at a temp dir, pins the clock, and stops
// the hook ignoring the test's temp files. It returns the config dir and
// a work dir for files the agent "edits".
func nudgeEnv(t *testing.T) (string, string) {
	t.Helper()
	cfg := useTempConfig(t)
	useFixedClock(t)
	old := nudgeIgnoredPrefixes
	nudgeIgnoredPrefixes = []string{filepath.Join(t.TempDir(), "ignored") + "/"}
	t.Cleanup(func() { nudgeIgnoredPrefixes = old })
	return cfg, t.TempDir()
}

// touch creates n files named f1.go .. fn.go under dir and returns their paths.
func touch(t *testing.T, dir string, n int) []string {
	t.Helper()
	var paths []string
	for i := 1; i <= n; i++ {
		p := filepath.Join(dir, fmt.Sprintf("f%d.go", i))
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	return paths
}

type nudgeCall struct {
	tool, path, agentID, subagentType string
}

// nudge runs the hook once and returns the injected context, or "" when
// nothing was printed.
func nudge(t *testing.T, c nudgeCall) string {
	t.Helper()
	p := map[string]any{
		"tool_name":  c.tool,
		"session_id": "sess-1",
		"tool_input": map[string]any{"file_path": c.path, "subagent_type": c.subagentType},
	}
	if c.agentID != "" {
		p["agent_id"] = c.agentID
	}
	payload, _ := json.Marshal(p)
	code, stdout, stderr := runOut(t, string(payload), "hook", "nudge")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if stdout == "" {
		return ""
	}
	var out struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil || !strings.HasSuffix(stdout, "}\n") {
		t.Fatalf("output is not one JSON line: %v %q", err, stdout)
	}
	if out.HookSpecificOutput.HookEventName != "PostToolUse" {
		t.Errorf("hookEventName = %q", out.HookSpecificOutput.HookEventName)
	}
	return out.HookSpecificOutput.AdditionalContext
}

func readState(t *testing.T, cfg, key string) nudgeState {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(cfg, "aec", "state", key+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var s nudgeState
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNudgeStateLoadFiresOnceAndRearms(t *testing.T) {
	cfg, dir := nudgeEnv(t)
	files := touch(t, dir, 6)
	for _, f := range files[:4] {
		if got := nudge(t, nudgeCall{tool: "Write", path: f}); got != "" {
			t.Fatalf("fired early: %q", got)
		}
	}
	if s := readState(t, cfg, "sess-1"); len(s.Dirty) != 4 || s.Streak != 4 {
		t.Fatalf("state: %+v", s)
	}
	got := nudge(t, nudgeCall{tool: "Edit", path: files[4]})
	want := "State-load nudge (automatic: your count of edited-but-not-reread files just reached 5).\n\n" +
		"You are carrying these files as remembered state:\n"
	for _, f := range files[:5] {
		want += "- " + f + " (first edited just now, not re-read since)\n"
	}
	want += "\nDrift is silent - an internal model goes stale before any action visibly fails, " +
		`so do not trust a feeling of "I remember these fine". Re-read each file above before ` +
		"editing further; a Read clears a file from this set, and this nudge stays quiet while the set stays below 5."
	if got != want {
		t.Fatalf("message:\n%s\nwant:\n%s", got, want)
	}
	if got := nudge(t, nudgeCall{tool: "Write", path: files[5]}); got != "" {
		t.Errorf("fired twice: %q", got)
	}
	nudge(t, nudgeCall{tool: "Read", path: files[0]})
	nudge(t, nudgeCall{tool: "Read", path: files[1]})
	if got := nudge(t, nudgeCall{tool: "Write", path: files[0]}); !strings.HasPrefix(got, "State-load nudge") {
		t.Errorf("did not re-arm: %q", got)
	}
	log, _ := os.ReadFile(filepath.Join(cfg, "aec", "state-nudge.log"))
	if !strings.Contains(string(log), "2026-10-09T19:30:12+01:00 | sess-1 | fired at 5 (threshold 5, model unknown) | "+files[0]+", ") {
		t.Errorf("log:\n%s", log)
	}
}

func TestNudgeConfidenceCheck(t *testing.T) {
	cfg, dir := nudgeEnv(t)
	writeOverlay(t, cfg, `disabled = ["state-load"]`)
	f := touch(t, dir, 1)[0]
	for i := 1; i < 12; i++ {
		if got := nudge(t, nudgeCall{tool: "Edit", path: f}); got != "" {
			t.Fatalf("edit %d fired: %q", i, got)
		}
	}
	got := nudge(t, nudgeCall{tool: "Edit", path: f})
	want := "Confidence check (automatic: 12 consecutive Write/Edit calls without a single Read - the first of them just now).\n\n" +
		"A long friction-free streak of edits is the exact condition where confident-momentum forms: " +
		"concerns get flagged then flowed past, and the session that feels like it's going best is the riskiest. " +
		"Before the next edit, do two things:\n" +
		"1. Name the riskiest thing you have asserted-but-not-verified during this streak, and verify it now - run the command, read the code. Do not reassure yourself from memory.\n" +
		"2. Sweep what you have written during the streak for exposure: real hostnames, internal IPs, people's names, secrets - none of those belong in code, tests, or notes.\n\n" +
		`Report briefly what you checked and what you found before carrying on. A bare "all fine" with nothing named is itself the flagged-then-flowed-past tell.`
	if got != want {
		t.Fatalf("message:\n%s\nwant:\n%s", got, want)
	}
	if got := nudge(t, nudgeCall{tool: "Edit", path: f}); got != "" {
		t.Errorf("fired twice")
	}
	nudge(t, nudgeCall{tool: "Read", path: f})
	if s := readState(t, cfg, "sess-1"); s.Streak != 0 || s.StreakStarted != nil || !s.ConfidenceArmed {
		t.Errorf("read should reset and re-arm: %+v", s)
	}
}

func TestNudgeDisabledSignalKeepsState(t *testing.T) {
	cfg, dir := nudgeEnv(t)
	writeOverlay(t, cfg, `disabled = ["state-load"]`)
	files := touch(t, dir, 5)
	for _, f := range files {
		if got := nudge(t, nudgeCall{tool: "Write", path: f}); got != "" {
			t.Fatalf("disabled signal fired: %q", got)
		}
	}
	if s := readState(t, cfg, "sess-1"); s.Armed || len(s.Dirty) != 5 {
		t.Errorf("state machine should run as if enabled: %+v", s)
	}
}

func TestNudgeOverlayThreshold(t *testing.T) {
	cfg, dir := nudgeEnv(t)
	writeOverlay(t, cfg, "[nudge]\ndirty_threshold = 3\n")
	files := touch(t, dir, 3)
	nudge(t, nudgeCall{tool: "Write", path: files[0]})
	nudge(t, nudgeCall{tool: "Write", path: files[1]})
	if got := nudge(t, nudgeCall{tool: "Write", path: files[2]}); !strings.Contains(got, "just reached 3") {
		t.Errorf("got %q", got)
	}
}

func TestNudgeIgnores(t *testing.T) {
	cfg, dir := nudgeEnv(t)
	ignored := nudgeIgnoredPrefixes[0]
	os.MkdirAll(ignored, 0o755)
	f := touch(t, dir, 1)[0]
	for _, c := range []nudgeCall{
		{tool: "Write", path: touch(t, ignored, 1)[0]},
		{tool: "Bash", path: f},
		{tool: "Write", path: ""},
		{tool: "Agent", subagentType: "general-purpose"},
	} {
		nudge(t, c)
	}
	if _, err := os.Stat(filepath.Join(cfg, "aec", "state", "sess-1.json")); err == nil {
		t.Errorf("ignored calls created state: %+v", readState(t, cfg, "sess-1"))
	}
	if nudgeIgnoredPrefixes[0] == "/tmp/" {
		t.Fatal("test setup")
	}
}

func TestNudgeProductionPrefixes(t *testing.T) {
	want := []string{"/tmp/", "/private/", "/var/folders/"}
	if fmt.Sprint(nudgeIgnoredPrefixes) != fmt.Sprint(want) {
		t.Errorf("prefixes = %v", nudgeIgnoredPrefixes)
	}
}

func TestNudgeDeletedFilesDropOut(t *testing.T) {
	cfg, dir := nudgeEnv(t)
	files := touch(t, dir, 3)
	for _, f := range files {
		nudge(t, nudgeCall{tool: "Write", path: f})
	}
	os.Remove(files[0])
	nudge(t, nudgeCall{tool: "Write", path: files[1]})
	if s := readState(t, cfg, "sess-1"); len(s.Dirty) != 2 {
		t.Errorf("dirty = %v", s.Dirty)
	}
}

func TestNudgeSubagentOwnState(t *testing.T) {
	cfg, dir := nudgeEnv(t)
	files := touch(t, dir, 2)
	nudge(t, nudgeCall{tool: "Write", path: files[0]})
	nudge(t, nudgeCall{tool: "Write", path: files[1], agentID: "ag/7"})
	if s := readState(t, cfg, "sess-1"); len(s.Dirty) != 1 {
		t.Errorf("parent: %v", s.Dirty)
	}
	if s := readState(t, cfg, "sess-1--ag_7"); len(s.Dirty) != 1 {
		t.Errorf("subagent: %v", s.Dirty)
	}
}

func TestNudgeNeverFails(t *testing.T) {
	cfg, dir := nudgeEnv(t)
	f := touch(t, dir, 1)[0]
	for _, payload := range []string{"not json", `{"tool_name":"Write","tool_input":{"file_path":"` + f + `"}}`} {
		code, stdout, stderr := runOut(t, payload, "hook", "nudge")
		if code != 0 || stdout != "" || stderr != "" {
			t.Errorf("%q: code=%d stdout=%q stderr=%q", payload, code, stdout, stderr)
		}
	}
	os.WriteFile(filepath.Join(cfg, "aec"), nil, 0o644)
	if got := nudge(t, nudgeCall{tool: "Write", path: f}); got != "" {
		t.Errorf("unwritable state dir: %q", got)
	}
	if code, _, _ := runOut(t, "{}", "hook", "nudge", "extra"); code != 64 {
		t.Errorf("extra argument: code=%d", code)
	}
}

func TestNudgeBrokenOverlayIsSilent(t *testing.T) {
	cfg, dir := nudgeEnv(t)
	writeOverlay(t, cfg, "disabled = [\n")
	files := touch(t, dir, 5)
	var got string
	for _, f := range files {
		got = nudge(t, nudgeCall{tool: "Write", path: f})
	}
	if !strings.HasPrefix(got, "State-load nudge") {
		t.Errorf("defaults should apply: %q", got)
	}
}

func TestNudgeSortsTiesByPath(t *testing.T) {
	s := map[string]int64{"/b": 5, "/a": 5, "/c": 1}
	if got := fmt.Sprint(byAge(s)); got != "[/c /a /b]" {
		t.Errorf("got %s", got)
	}
}

func TestMinutesAgo(t *testing.T) {
	for _, c := range []struct {
		ago  int64
		want string
	}{{0, "just now"}, {59, "just now"}, {60, "1m ago"}, {600, "10m ago"}, {-5, "just now"}} {
		if got := minutesAgo(1000-c.ago, 1000); got != c.want {
			t.Errorf("%d: got %q want %q", c.ago, got, c.want)
		}
	}
}
