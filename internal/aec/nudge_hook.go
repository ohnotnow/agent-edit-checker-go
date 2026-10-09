package aec

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// userHomeDir finds the home directory for the global agents folder.
// Tests replace it.
var userHomeDir = os.UserHomeDir

// nudgeIgnoredPrefixes are temp locations whose files are scratch output,
// not state the agent must keep in sync. Tests replace it.
var nudgeIgnoredPrefixes = []string{"/tmp/", "/private/", "/var/folders/"}

// nudgePayload is the part of Claude Code's PostToolUse JSON the nudge
// hook uses.
type nudgePayload struct {
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		FilePath     string `json:"file_path"`
		SubagentType string `json:"subagent_type"`
	} `json:"tool_input"`
	SessionID      string `json:"session_id"`
	AgentID        string `json:"agent_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
}

var stateKeyRe = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// runNudge is the nudge hook. It tracks files edited but not re-read and
// runs of edits without a Read, per session, and when a signal fires it
// prints the message as PostToolUse additional context. It never fails:
// any error or panic ends it quietly.
func runNudge(stdin io.Reader, stdout io.Writer) {
	defer func() { _ = recover() }()

	var p nudgePayload
	if err := json.NewDecoder(stdin).Decode(&p); err != nil || p.SessionID == "" {
		return
	}
	path := p.ToolInput.FilePath
	var cfg NudgeSettings
	var signals []Signal
	if p.ToolName == "Agent" {
		cfg, signals = nudgeConfig()
		if p.ToolInput.SubagentType != cfg.TestAgent {
			return
		}
	} else {
		if path == "" || !slices.Contains([]string{"Write", "Edit", "Read"}, p.ToolName) {
			return
		}
		for _, prefix := range nudgeIgnoredPrefixes {
			if strings.HasPrefix(path, prefix) {
				return
			}
		}
		cfg, signals = nudgeConfig()
	}
	key := stateKeyRe.ReplaceAllString(p.SessionID+suffixIf("--", p.AgentID), "_")
	dir, err := configDir()
	if err != nil {
		return
	}
	stateDir := filepath.Join(dir, "aec", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return
	}
	stateFile := filepath.Join(stateDir, key+".json")
	f, err := os.OpenFile(stateFile, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	if err := lockFile(f); err != nil {
		return
	}
	defer unlockFile(f)

	state := readNudgeState(f, cfg)
	logFile, _ := logPath("state-nudge.log")
	if p.ToolName == "Agent" {
		cleared := len(state.TestFiles)
		state.TestFiles = map[string]int64{}
		state.TestNextFire = cfg.TestFiles
		if writeNudgeState(f, state) == nil && cleared > 0 {
			appendLog(logFile, key, "test-nudge-reset ("+cfg.TestAgent+" launched; "+strconv.Itoa(cleared)+" test files cleared)")
		}
		return
	}

	nowUnix := now().Unix()
	state.record(p.ToolName, path, nowUnix)
	if p.ToolName != "Read" && strings.HasSuffix(path, ".php") && strings.Contains(path, "/tests/") {
		if root, ok := findUp(filepath.Dir(path), "artisan"); ok && strings.HasPrefix(path, root+"/tests/") {
			if _, seen := state.TestFiles[path]; !seen {
				state.TestFiles[path] = nowUnix
			}
		}
	}
	state.prune(isRegularFile)
	enabled := func(name string) bool {
		return slices.ContainsFunc(signals, func(s Signal) bool { return s.Name == name && s.Enabled })
	}
	threshold, model := cfg.DirtyThreshold, ""
	if p.AgentID == "" && enabled("state-load") && len(state.Dirty) >= cfg.lowestThreshold() {
		model = detectModel(p.TranscriptPath)
		threshold = cfg.ThresholdFor(model)
	}
	dirtyFire, confidenceFire := state.evaluate(threshold, cfg.ConfidenceStreak)

	testFire := false
	if state.ratchetTests(cfg.TestFiles) && enabled("test-quality") {
		if testAgentInstalled(cfg.TestAgent, byAge(state.TestFiles)[0]) {
			testFire = true
			state.TestFired++
		} else {
			appendLog(logFile, key, "test-nudge-skipped at "+strconv.Itoa(len(state.TestFiles))+" test files - no "+cfg.TestAgent+" agent installed (see https://github.com/ohnotnow/agentic-stuff)")
		}
	}
	if err := writeNudgeState(f, state); err != nil {
		return
	}

	var messages []string
	if dirtyFire && enabled("state-load") {
		messages = append(messages, stateLoadMessage(state, threshold, nowUnix))
		label := model
		switch {
		case label != "":
		case p.AgentID != "":
			label = "subagent-default"
		default:
			label = "unknown"
		}
		appendLog(logFile, key,
			"fired at "+strconv.Itoa(len(state.Dirty))+" (threshold "+strconv.Itoa(threshold)+", model "+label+")",
			strings.Join(byAge(state.Dirty), ", "))
	}
	if confidenceFire && enabled("confidence-check") {
		messages = append(messages, confidenceMessage(state, nowUnix))
		appendLog(logFile, key, "confidence-fired at streak "+strconv.Itoa(state.Streak))
	}
	if testFire {
		messages = append(messages, testQualityMessage(state, cfg.TestAgent, nowUnix))
		appendLog(logFile, key,
			"test-nudge-fired at "+strconv.Itoa(len(state.TestFiles))+" test files (firing #"+strconv.Itoa(state.TestFired)+")",
			strings.Join(byAge(state.TestFiles), ", "))
	}
	if len(messages) == 0 {
		return
	}
	message := strings.Join(messages, "\n\n") + aitClause(p.Cwd)
	pruneState(stateDir, stateFile)
	out, err := json.Marshal(map[string]any{"hookSpecificOutput": map[string]string{
		"hookEventName":     "PostToolUse",
		"additionalContext": message,
	}})
	if err != nil {
		return
	}
	_, _ = stdout.Write(append(out, '\n'))
}

// nudgeConfig returns the effective nudge settings and signals, or the
// defaults with every signal on when the overlay cannot be used. It
// prints nothing.
func nudgeConfig() (NudgeSettings, []Signal) {
	defaults, err := LoadDefaults()
	if err == nil {
		if merged, err := loadMerged(defaults); err == nil {
			return merged.Nudge, merged.Signals
		}
	}
	cfg, err := LoadDefaultNudge()
	if err != nil {
		panic(err) // the embedded file is validated by the test suite
	}
	return cfg, mergeSignals(nil)
}

func readNudgeState(f *os.File, cfg NudgeSettings) *nudgeState {
	var s nudgeState
	data, err := io.ReadAll(f)
	if err != nil || json.Unmarshal(data, &s) != nil || s.Dirty == nil {
		return newNudgeState(cfg)
	}
	if s.TestFiles == nil {
		s.TestFiles = map[string]int64{}
	}
	return &s
}

func writeNudgeState(f *os.File, s *nudgeState) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return err
}

// testAgentInstalled reports whether the reviewer agent's file exists in
// the global agents folder or in the Laravel project of testFile.
func testAgentInstalled(agent, testFile string) bool {
	file := filepath.Join(".claude", "agents", agent+".md")
	if home, err := userHomeDir(); err == nil && home != "" && isRegularFile(filepath.Join(home, file)) {
		return true
	}
	root, ok := findUp(filepath.Dir(testFile), "artisan")
	return ok && isRegularFile(filepath.Join(root, file))
}

// findUp returns the nearest of dir and its ancestors holding the regular
// file rel.
func findUp(dir, rel string) (string, bool) {
	for {
		if isRegularFile(filepath.Join(dir, rel)) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func suffixIf(sep, s string) string {
	if s == "" {
		return ""
	}
	return sep + s
}
