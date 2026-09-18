package aec

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// payload is the part of Claude Code's PreToolUse JSON the hooks use.
type payload struct {
	ToolInput struct {
		FilePath  string  `json:"file_path"`
		Content   *string `json:"content"`
		NewString *string `json:"new_string"`
		OldString string  `json:"old_string"`
		Command   string  `json:"command"`
	} `json:"tool_input"`
}

func (p payload) content() string {
	if p.ToolInput.Content != nil {
		return *p.ToolInput.Content
	}
	if p.ToolInput.NewString != nil {
		return *p.ToolInput.NewString
	}
	return ""
}

type hookKind int

const (
	hookEdit hookKind = iota
	hookBash
)

// hookCommand wraps runHook as a command. The hooks take no arguments.
func hookCommand(kind hookKind) command {
	return func(args []string, stdin io.Reader, _, stderr io.Writer) error {
		if len(args) != 0 {
			return usageErr("hook commands take no arguments")
		}
		return runHook(kind, stdin, stderr)
	}
}

// runHook loads the effective rules, decodes the payload and prints any
// violations. Unreadable input allows the call, as the PHP hooks did.
// A blocked call returns errBlocked after the Blocked lines are printed.
func runHook(kind hookKind, stdin io.Reader, stderr io.Writer) error {
	rules := effectiveRules(stderr)

	var p payload
	if err := json.NewDecoder(stdin).Decode(&p); err != nil {
		return nil
	}

	var msgs []string
	switch kind {
	case hookEdit:
		msgs = CheckContent(rules, FileType(p.ToolInput.FilePath), p.content(), p.ToolInput.OldString)
	case hookBash:
		msgs = CheckCommand(rules, p.ToolInput.Command)
		logCommand(len(msgs) > 0, p.ToolInput.Command)
	}

	if len(msgs) == 0 {
		return nil
	}
	for _, m := range msgs {
		fmt.Fprintf(stderr, "\u274c Blocked: %s\n", m)
	}
	return errBlocked
}

// effectiveRules merges the overlay over the defaults. A broken overlay is
// reported on stderr and ignored so the defaults still apply.
func effectiveRules(stderr io.Writer) []Rule {
	defaults, err := LoadDefaults()
	if err != nil {
		panic(err) // the embedded file is validated by the test suite
	}
	merged, err := loadMerged(defaults)
	if err != nil {
		fmt.Fprintf(stderr, "aec: overlay ignored: %v\n", err)
		return defaults
	}
	return merged.Rules
}

func loadMerged(defaults []Rule) (Merged, error) {
	path, err := OverlayPath()
	if err != nil {
		return Merged{}, err
	}
	ov, err := LoadOverlay(path)
	if err != nil {
		return Merged{}, err
	}
	return Merge(defaults, ov)
}

func logCommand(denied bool, command string) {
	dir, err := configDir()
	if err != nil {
		return
	}
	dir = filepath.Join(dir, "aec")
	_ = os.MkdirAll(dir, 0o755)
	LogDecision(filepath.Join(dir, "tool-use.log"), denied, command)
}
