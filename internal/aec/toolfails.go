package aec

import (
	"bytes"
	"encoding/json"
	"io"
)

// failurePayload is the part of Claude Code's PostToolUseFailure JSON the
// tool-fails hook uses.
type failurePayload struct {
	ToolName    string          `json:"tool_name"`
	ToolInput   json.RawMessage `json:"tool_input"`
	Error       string          `json:"error"`
	IsInterrupt bool            `json:"is_interrupt"`
}

// detail is the command for Bash, the file path for file tools, and the
// whole tool input as compact JSON for anything else.
func (p failurePayload) detail() string {
	var in struct {
		Command  string `json:"command"`
		FilePath string `json:"file_path"`
	}
	_ = json.Unmarshal(p.ToolInput, &in)
	switch {
	case in.Command != "":
		return in.Command
	case in.FilePath != "":
		return in.FilePath
	case len(p.ToolInput) == 0 || string(p.ToolInput) == "null":
		return "{}"
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, p.ToolInput); err != nil {
		return string(p.ToolInput)
	}
	return buf.String()
}

// logToolFailure appends one line per failed tool call to tool-fails.log:
// timestamp, tool name, detail and error. Interrupts (the user pressed
// escape) and unreadable input are skipped. It never fails.
func logToolFailure(stdin io.Reader) {
	var p failurePayload
	if err := json.NewDecoder(stdin).Decode(&p); err != nil || p.IsInterrupt {
		return
	}
	if p.ToolName == "" {
		p.ToolName = "unknown"
	}
	if path, ok := logPath("tool-fails.log"); ok {
		appendLog(path, p.ToolName, p.detail(), p.Error)
	}
}
