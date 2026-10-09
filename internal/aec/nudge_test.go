package aec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	oldRun := runCommand
	runCommand = func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("no commands in tests")
	}
	t.Cleanup(func() { nudgeIgnoredPrefixes, runCommand = old, oldRun })
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
	tool, path, agentID, subagentType, transcript, cwd string
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
	if c.transcript != "" {
		p["transcript_path"] = c.transcript
	}
	if c.cwd != "" {
		p["cwd"] = c.cwd
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

// laravelApp makes <dir>/app with an artisan file and n test files under
// tests/Feature, and a fake home holding the reviewer agent when withAgent.
func laravelApp(t *testing.T, dir string, n int, withAgent bool, agent string) (root string, tests []string) {
	t.Helper()
	root = filepath.Join(dir, "app")
	os.MkdirAll(filepath.Join(root, "tests", "Feature"), 0o755)
	os.WriteFile(filepath.Join(root, "artisan"), nil, 0o644)
	for i := 1; i <= n; i++ {
		p := filepath.Join(root, "tests", "Feature", fmt.Sprintf("T%02dTest.php", i))
		os.WriteFile(p, nil, 0o644)
		tests = append(tests, p)
	}
	home := t.TempDir()
	if withAgent {
		os.MkdirAll(filepath.Join(home, ".claude", "agents"), 0o755)
		os.WriteFile(filepath.Join(home, ".claude", "agents", agent+".md"), nil, 0o644)
	}
	old := userHomeDir
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = old })
	return root, tests
}

// editAll edits each path once with reads in between, so only the
// test-quality signal can fire, and returns the last injected context.
func editAll(t *testing.T, paths []string) []string {
	t.Helper()
	var got []string
	for _, p := range paths {
		got = append(got, nudge(t, nudgeCall{tool: "Write", path: p}))
		nudge(t, nudgeCall{tool: "Read", path: p})
	}
	return got
}

func TestNudgeTestQualityFiresAndRatchets(t *testing.T) {
	cfg, dir := nudgeEnv(t)
	_, tests := laravelApp(t, dir, 16, true, "test-quality-checker")
	got := editAll(t, tests)
	for i, g := range got {
		if (i == 7 || i == 15) != (g != "") {
			t.Fatalf("file %d: fired=%v", i+1, g != "")
		}
	}
	want := "Test-quality nudge (automatic: 8 distinct test files edited since the last fresh-eyes review).\n\n" +
		"A fresh pair of eyes catches the anti-patterns you have stopped seeing - and a habit corrected now is one the rest of the session does not repeat.\n\n" +
		"The test files in question:\n"
	for _, p := range tests[:8] {
		want += "- " + p + " (first edited just now)\n"
	}
	want += "\nLaunch the test-quality-checker subagent now: hand it this file list and tell it this is a mid-feature WIP review, so it should judge the tests that exist rather than flag incompleteness. " +
		`Act on any "you really should fix" findings before writing more tests - launching the checker is what resets this counter.`
	if got[7] != want {
		t.Fatalf("message:\n%s\nwant:\n%s", got[7], want)
	}
	if !strings.Contains(got[15], "\n\nFrom the inside every test you write looks fine;") {
		t.Errorf("second firing should use phrasing 2:\n%s", got[15])
	}
	log, _ := os.ReadFile(filepath.Join(cfg, "aec", "state-nudge.log"))
	if !strings.Contains(string(log), "| sess-1 | test-nudge-fired at 8 test files (firing #1) | "+tests[0]+", ") {
		t.Errorf("log:\n%s", log)
	}
}

func TestNudgeTestQualityCountsOnlyLaravelTests(t *testing.T) {
	cfg, dir := nudgeEnv(t)
	root, tests := laravelApp(t, dir, 1, true, "test-quality-checker")
	other := filepath.Join(dir, "plain", "tests")
	os.MkdirAll(other, 0o755)
	notCounted := []string{filepath.Join(root, "app", "Thing.php"), filepath.Join(root, "tests", "notes.md"), filepath.Join(other, "XTest.php")}
	os.MkdirAll(filepath.Join(root, "app"), 0o755)
	for _, p := range notCounted {
		os.WriteFile(p, nil, 0o644)
	}
	editAll(t, append(notCounted, tests...))
	if s := readState(t, cfg, "sess-1"); len(s.TestFiles) != 1 || s.TestFiles[tests[0]] == 0 {
		t.Errorf("test_files = %v", s.TestFiles)
	}
}

func TestNudgeTestQualitySkipsWithoutAgent(t *testing.T) {
	cfg, dir := nudgeEnv(t)
	_, tests := laravelApp(t, dir, 8, false, "")
	for i, g := range editAll(t, tests) {
		if g != "" {
			t.Fatalf("file %d fired without an agent installed", i+1)
		}
	}
	if s := readState(t, cfg, "sess-1"); s.TestNextFire != 16 || s.TestFired != 0 {
		t.Errorf("state: %+v", s)
	}
	log, _ := os.ReadFile(filepath.Join(cfg, "aec", "state-nudge.log"))
	if !strings.Contains(string(log), "| sess-1 | test-nudge-skipped at 8 test files - no test-quality-checker agent installed (see https://github.com/ohnotnow/agentic-stuff)") {
		t.Errorf("log:\n%s", log)
	}
}

func TestNudgeTestQualityProjectAgent(t *testing.T) {
	_, dir := nudgeEnv(t)
	root, tests := laravelApp(t, dir, 8, false, "")
	os.MkdirAll(filepath.Join(root, ".claude", "agents"), 0o755)
	os.WriteFile(filepath.Join(root, ".claude", "agents", "test-quality-checker.md"), nil, 0o644)
	if got := editAll(t, tests); got[7] == "" {
		t.Error("project-level agent should count as installed")
	}
}

func TestNudgeTestQualityReviewResets(t *testing.T) {
	cfg, dir := nudgeEnv(t)
	_, tests := laravelApp(t, dir, 3, true, "test-quality-checker")
	editAll(t, tests)
	if got := nudge(t, nudgeCall{tool: "Agent", subagentType: "test-quality-checker"}); got != "" {
		t.Errorf("review launch printed %q", got)
	}
	if s := readState(t, cfg, "sess-1"); len(s.TestFiles) != 0 || s.TestNextFire != 8 {
		t.Errorf("state: %+v", s)
	}
	log, _ := os.ReadFile(filepath.Join(cfg, "aec", "state-nudge.log"))
	if !strings.Contains(string(log), "| sess-1 | test-nudge-reset (test-quality-checker launched; 3 test files cleared)") {
		t.Errorf("log:\n%s", log)
	}
	nudge(t, nudgeCall{tool: "Agent", subagentType: "test-quality-checker"})
	log2, _ := os.ReadFile(filepath.Join(cfg, "aec", "state-nudge.log"))
	if strings.Count(string(log2), "test-nudge-reset") != 1 {
		t.Errorf("an empty reset should not log:\n%s", log2)
	}
}

func TestNudgeTestQualitySettings(t *testing.T) {
	cfg, dir := nudgeEnv(t)
	writeOverlay(t, cfg, "[nudge]\ntest_files = 3\ntest_agent = \"my-reviewer\"\n")
	_, tests := laravelApp(t, dir, 3, true, "my-reviewer")
	got := editAll(t, tests)
	if got[2] == "" || !strings.Contains(got[2], "Launch the my-reviewer subagent now") {
		t.Fatalf("did not fire at 3 with my-reviewer: %q", got[2])
	}
	nudge(t, nudgeCall{tool: "Agent", subagentType: "test-quality-checker"})
	if s := readState(t, cfg, "sess-1"); len(s.TestFiles) != 3 {
		t.Error("the default agent name should not reset a renamed reviewer")
	}
	nudge(t, nudgeCall{tool: "Agent", subagentType: "my-reviewer"})
	if s := readState(t, cfg, "sess-1"); len(s.TestFiles) != 0 {
		t.Error("my-reviewer should reset")
	}
}

func TestNudgeTestQualityDisabled(t *testing.T) {
	cfg, dir := nudgeEnv(t)
	writeOverlay(t, cfg, `disabled = ["test-quality"]`)
	_, tests := laravelApp(t, dir, 8, true, "test-quality-checker")
	if got := editAll(t, tests); got[7] != "" {
		t.Errorf("disabled signal fired: %q", got[7])
	}
}

// transcript writes a fake session transcript whose last model line names
// model, followed by filler bytes with no model line.
func transcript(t *testing.T, model string, filler int) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "session.jsonl")
	body := `{"type":"assistant","message":{"model":"claude-haiku-4-5"}}` + "\n" +
		`{"type":"assistant","message":{"model":"` + model + `"}}` + "\n" +
		`{"type":"user","content":"` + strings.Repeat("A", filler) + `"}` + "\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// firesAt edits distinct files, reading nothing, until state-load fires,
// and returns the count it fired at, or 0.
func firesAt(t *testing.T, dir string, c nudgeCall, max int) int {
	t.Helper()
	for i, f := range touch(t, dir, max) {
		c.tool, c.path = "Write", f
		if strings.HasPrefix(nudge(t, c), "State-load nudge") {
			return i + 1
		}
	}
	return 0
}

func TestNudgeModelThresholds(t *testing.T) {
	cases := []struct {
		name, overlay, model string
		filler, want         int
	}{
		{"fable", "", "claude-fable-5-1", 0, 10},
		{"haiku", "", "claude-haiku-4-5", 0, 5},
		{"sub-version override", "[nudge.models]\n\"fable-5-1\" = 6\n", "claude-fable-5-1", 0, 6},
		{"below the default", "[nudge.models]\nhaiku = 3\n", "claude-haiku-4-5", 0, 3},
		{"model line behind a 1 MB line", "", "claude-fable-5-1", 1 << 20, 10},
		{"no model within 4 MB", "", "claude-fable-5-1", 5 << 20, 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, dir := nudgeEnv(t)
			if c.overlay != "" {
				writeOverlay(t, cfg, c.overlay)
			}
			call := nudgeCall{transcript: transcript(t, c.model, c.filler)}
			if got := firesAt(t, dir, call, 12); got != c.want {
				t.Errorf("fired at %d, want %d", got, c.want)
			}
		})
	}
}

func TestNudgeModelLogLabel(t *testing.T) {
	cfg, dir := nudgeEnv(t)
	firesAt(t, dir, nudgeCall{transcript: transcript(t, "claude-fable-5-1", 0)}, 10)
	firesAt(t, t.TempDir(), nudgeCall{transcript: filepath.Join(dir, "missing.jsonl"), agentID: "x"}, 5)
	log, _ := os.ReadFile(filepath.Join(cfg, "aec", "state-nudge.log"))
	for _, s := range []string{"fired at 10 (threshold 10, model claude-fable-5-1)", "fired at 5 (threshold 5, model subagent-default)"} {
		if !strings.Contains(string(log), s) {
			t.Errorf("log lacks %q:\n%s", s, log)
		}
	}
}

func TestNudgeSniffOnlyWhenNeeded(t *testing.T) {
	cfg, dir := nudgeEnv(t)
	old := detectModel
	t.Cleanup(func() { detectModel = old })
	detectModel = func(string) string {
		t.Fatal("transcript read when it should not be")
		return ""
	}
	files := touch(t, dir, 5)
	for _, f := range files[:4] {
		nudge(t, nudgeCall{tool: "Write", path: f, transcript: "x"})
	}
	for _, f := range files {
		nudge(t, nudgeCall{tool: "Write", path: f, transcript: "x", agentID: "sub"})
	}
	writeOverlay(t, cfg, `disabled = ["state-load"]`)
	nudge(t, nudgeCall{tool: "Write", path: files[4], transcript: "x"})
}

func TestNudgeAitClause(t *testing.T) {
	cfg, dir := nudgeEnv(t)
	writeOverlay(t, cfg, "[nudge]\ndirty_threshold = 1\n")
	project := filepath.Join(dir, "proj")
	os.MkdirAll(filepath.Join(project, ".ait"), 0o755)
	os.MkdirAll(filepath.Join(project, "sub"), 0o755)
	os.WriteFile(filepath.Join(project, ".ait", "ait.db"), nil, 0o644)
	old := runCommand
	t.Cleanup(func() { runCommand = old })
	var gotArgs []string
	reply := `{"issues":[{"id":"x-1","title":"First"},{"id":"x-2","title":"Second"}]}`
	runCommand = func(_ context.Context, name string, args ...string) ([]byte, error) {
		gotArgs = append([]string{name}, args...)
		return []byte(reply), nil
	}
	f := touch(t, dir, 3)
	got := nudge(t, nudgeCall{tool: "Write", path: f[0], cwd: filepath.Join(project, "sub")})
	want := "\n\nAlso re-read your in-progress ait issues to re-anchor on the goal and acceptance criteria:\n" +
		"- x-1: First (re-read with: ait show x-1)\n- x-2: Second (re-read with: ait show x-2)"
	if !strings.HasSuffix(got, want) {
		t.Errorf("message end:\n%q", got)
	}
	if fmt.Sprint(gotArgs) != fmt.Sprint([]string{"ait", "--db", filepath.Join(project, ".ait", "ait.db"), "list", "--status", "in_progress"}) {
		t.Errorf("args = %v", gotArgs)
	}
	for _, r := range []string{`{"issues":[{"id":"x-1","title":"Only"}]}`, `{"issues":[]}`, `not json`} {
		reply = r
		nudge(t, nudgeCall{tool: "Read", path: f[0]})
		got := nudge(t, nudgeCall{tool: "Write", path: f[0], cwd: project})
		wantClause := r == `{"issues":[{"id":"x-1","title":"Only"}]}`
		if strings.Contains(got, "in-progress ait") != wantClause {
			t.Errorf("%s: %q", r, got)
		}
		if wantClause && !strings.Contains(got, "in-progress ait issue to") {
			t.Errorf("singular noun: %q", got)
		}
	}
	runCommand = func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("boom") }
	nudge(t, nudgeCall{tool: "Read", path: f[0]})
	if got := nudge(t, nudgeCall{tool: "Write", path: f[0], cwd: project}); strings.Contains(got, "ait") {
		t.Errorf("failed command: %q", got)
	}
}

func TestNudgePrunesOldState(t *testing.T) {
	cfg, dir := nudgeEnv(t)
	stateDir := filepath.Join(cfg, "aec", "state")
	os.MkdirAll(stateDir, 0o755)
	old, young := filepath.Join(stateDir, "old.json"), filepath.Join(stateDir, "young.json")
	for p, age := range map[string]time.Duration{old: 49 * time.Hour, young: time.Hour} {
		os.WriteFile(p, []byte("{}"), 0o644)
		when := now().Add(-age)
		os.Chtimes(p, when, when)
	}
	files := touch(t, dir, 5)
	for _, f := range files[:4] {
		nudge(t, nudgeCall{tool: "Write", path: f})
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatal("pruned on a quiet call")
	}
	nudge(t, nudgeCall{tool: "Write", path: files[4]})
	if _, err := os.Stat(old); err == nil {
		t.Error("old state not pruned")
	}
	if _, err := os.Stat(young); err != nil {
		t.Error("young state pruned")
	}
	if _, err := os.Stat(filepath.Join(stateDir, "sess-1.json")); err != nil {
		t.Error("current state pruned")
	}
}

func TestLookPathExpandsTilde(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, "tools"), 0o755)
	tool := filepath.Join(home, "tools", "aec-probe-tool")
	os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o755)
	old := userHomeDir
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = old })
	t.Setenv("PATH", "/nonexistent:~/tools")
	if got := lookPath("aec-probe-tool"); got != tool {
		t.Errorf("got %q, want %q", got, tool)
	}
	if got := lookPath("aec-missing-tool"); got != "aec-missing-tool" {
		t.Errorf("missing: got %q", got)
	}
}
