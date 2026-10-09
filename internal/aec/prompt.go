package aec

import (
	"encoding/json"
	"io"
	"strings"
)

// CheckPrompt returns the messages of every enabled prompt rule whose
// pattern matches the prompt, in rule order.
func CheckPrompt(rules []Rule, prompt string) []string {
	var msgs []string
	for _, r := range rules {
		if r.Enabled && r.Prompt && matches(r.re, prompt) {
			msgs = append(msgs, r.Message)
		}
	}
	return msgs
}

// runPrompt prints the messages of the prompt rules that match the
// submitted prompt on stdout, a blank line between them, for Claude Code
// to add to the agent's context. It never blocks: unreadable input or no
// match prints nothing.
func runPrompt(stdin io.Reader, stdout, stderr io.Writer) {
	rules := effectiveRules(stderr)

	var p struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(stdin).Decode(&p); err != nil {
		return
	}
	if msgs := CheckPrompt(rules, p.Prompt); len(msgs) > 0 {
		_, _ = io.WriteString(stdout, strings.Join(msgs, "\n\n")+"\n")
	}
}
