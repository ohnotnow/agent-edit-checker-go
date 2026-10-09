package aec

import (
	"os"
	"strings"
	"time"
)

// CheckCommand returns the messages of every enabled rule that fires on a
// Bash command, in rule order. A rule is consulted only when its command
// pattern matches; a forbid rule fires when its pattern matches, a require
// rule when it does not.
func CheckCommand(rules []Rule, command string) []string {
	var msgs []string
	for _, r := range rules {
		if !r.Enabled || r.commandRe == nil || !matches(r.commandRe, command) {
			continue
		}
		matched := matches(r.re, command)
		if r.Type == "require" && !matched || r.Type != "require" && matched {
			msgs = append(msgs, r.Message)
		}
	}
	return msgs
}

var logLineReplacer = strings.NewReplacer("\r", `\r`, "\n", `\n`)

// LogDecision appends "<timestamp> | allowed | cmd" or
// "<timestamp> | denied | cmd" to the log at logPath. Best effort: any
// error is ignored so logging can never change the decision.
func LogDecision(logPath string, denied bool, command string) {
	decision := "allowed"
	if denied {
		decision = "denied"
	}
	appendLog(logPath, decision, command)
}

// appendLog appends one line to the log at logPath: the RFC 3339 local
// time, then each field, separated by " | ", with CR and LF in the fields
// written as \r and \n. Best effort: any error is ignored.
func appendLog(logPath string, fields ...string) {
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	line := now().Format(time.RFC3339)
	for _, field := range fields {
		line += " | " + logLineReplacer.Replace(field)
	}
	_, _ = f.WriteString(line + "\n")
}
