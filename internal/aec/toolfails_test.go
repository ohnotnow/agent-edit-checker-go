package aec

import (
	"os"
	"path/filepath"
	"testing"
)

// readToolFails returns the tool-fails log under dir, or "" when it does
// not exist.
func readToolFails(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "aec", "tool-fails.log"))
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestHookToolFailsLogs(t *testing.T) {
	const ts = "2026-10-09T19:30:12+01:00 | "
	tests := []struct {
		name, payload, want string
	}{
		{
			"bash command",
			`{"tool_name":"Bash","tool_input":{"command":"ls /nope"},"error":"Exit code 1"}`,
			ts + "Bash | ls /nope | Exit code 1\n",
		},
		{
			"file path",
			`{"tool_name":"Read","tool_input":{"file_path":"/x/missing.go"},"error":"File does not exist."}`,
			ts + "Read | /x/missing.go | File does not exist.\n",
		},
		{
			"other input as json",
			`{"tool_name":"mcp__notes__search","tool_input":{ "query": "cats", "limit": 3 },"error":"timeout"}`,
			ts + `mcp__notes__search | {"query":"cats","limit":3} | timeout` + "\n",
		},
		{
			"no tool input",
			`{"tool_name":"Mystery","error":"boom"}`,
			ts + "Mystery | {} | boom\n",
		},
		{
			"missing tool name",
			`{"tool_input":{"command":"ls"},"error":"x"}`,
			ts + "unknown | ls | x\n",
		},
		{
			"newlines flattened",
			`{"tool_name":"Bash","tool_input":{"command":"ls\na"},"error":"line one\r\nline two"}`,
			ts + `Bash | ls\na | line one\r\nline two` + "\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := useTempConfig(t)
			useFixedClock(t)
			code, stderr := run(t, tt.payload, "hook", "tool-fails")
			if code != 0 || stderr != "" {
				t.Errorf("code=%d stderr=%q", code, stderr)
			}
			if got := readToolFails(t, dir); got != tt.want {
				t.Errorf("log = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHookToolFailsWritesNothing(t *testing.T) {
	for name, payload := range map[string]string{
		"interrupt":    `{"tool_name":"Bash","tool_input":{"command":"ls"},"error":"x","is_interrupt":true}`,
		"invalid json": "not json",
	} {
		t.Run(name, func(t *testing.T) {
			dir := useTempConfig(t)
			code, stderr := run(t, payload, "hook", "tool-fails")
			if code != 0 || stderr != "" {
				t.Errorf("code=%d stderr=%q", code, stderr)
			}
			if got := readToolFails(t, dir); got != "" {
				t.Errorf("log = %q, want nothing", got)
			}
		})
	}
}

func TestHookToolFailsIgnoresOverlay(t *testing.T) {
	dir := useTempConfig(t)
	writeOverlay(t, dir, "disabled = [\n")
	code, stderr := run(t, `{"tool_name":"Bash","tool_input":{"command":"ls"},"error":"x"}`, "hook", "tool-fails")
	if code != 0 || stderr != "" {
		t.Errorf("code=%d stderr=%q", code, stderr)
	}
}

func TestHookToolFailsTakesNoArguments(t *testing.T) {
	useTempConfig(t)
	if code, _ := run(t, "{}", "hook", "tool-fails", "extra"); code != 64 {
		t.Errorf("code=%d, want 64", code)
	}
}
