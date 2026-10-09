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
	if path == "" || !slices.Contains([]string{"Write", "Edit", "Read"}, p.ToolName) {
		return
	}
	for _, prefix := range nudgeIgnoredPrefixes {
		if strings.HasPrefix(path, prefix) {
			return
		}
	}

	cfg, signals := nudgeConfig()
	key := stateKeyRe.ReplaceAllString(p.SessionID+suffixIf("--", p.AgentID), "_")
	dir, err := configDir()
	if err != nil {
		return
	}
	stateDir := filepath.Join(dir, "aec", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(stateDir, key+".json"), os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	if err := lockFile(f); err != nil {
		return
	}
	defer unlockFile(f)

	state := readNudgeState(f, cfg)
	nowUnix := now().Unix()
	state.record(p.ToolName, path, nowUnix)
	state.prune(isRegularFile)
	threshold := cfg.DirtyThreshold
	dirtyFire, confidenceFire := state.evaluate(threshold, cfg.ConfidenceStreak)
	if err := writeNudgeState(f, state); err != nil {
		return
	}

	enabled := func(name string) bool {
		return slices.ContainsFunc(signals, func(s Signal) bool { return s.Name == name && s.Enabled })
	}
	logFile, _ := logPath("state-nudge.log")
	var messages []string
	if dirtyFire && enabled("state-load") {
		messages = append(messages, stateLoadMessage(state, threshold, nowUnix))
		label := "unknown"
		if p.AgentID != "" {
			label = "subagent-default"
		}
		appendLog(logFile, key,
			"fired at "+strconv.Itoa(len(state.Dirty))+" (threshold "+strconv.Itoa(threshold)+", model "+label+")",
			strings.Join(byAge(state.Dirty), ", "))
	}
	if confidenceFire && enabled("confidence-check") {
		messages = append(messages, confidenceMessage(state, nowUnix))
		appendLog(logFile, key, "confidence-fired at streak "+strconv.Itoa(state.Streak))
	}
	if len(messages) == 0 {
		return
	}
	out, err := json.Marshal(map[string]any{"hookSpecificOutput": map[string]string{
		"hookEventName":     "PostToolUse",
		"additionalContext": strings.Join(messages, "\n\n"),
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
