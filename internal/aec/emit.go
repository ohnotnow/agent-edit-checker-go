package aec

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// emitRule writes r as one [[rules]] block followed by a blank line. Keys
// appear in this order and only when set: name, files, command, type,
// pattern, max_matches, message. Patterns are literal strings so they
// paste back into an overlay unchanged.
func emitRule(w io.Writer, r Rule) error {
	var b strings.Builder
	b.WriteString("[[rules]]\n")
	fmt.Fprintf(&b, "name = %s\n", basicString(r.Name))
	if len(r.Files) > 0 {
		quoted := make([]string, len(r.Files))
		for i, f := range r.Files {
			quoted[i] = basicString(f)
		}
		fmt.Fprintf(&b, "files = [%s]\n", strings.Join(quoted, ", "))
	}
	if r.Command != "" {
		s, err := literalString(r.Command)
		if err != nil {
			return fmt.Errorf("rule %q: command: %w", r.Name, err)
		}
		fmt.Fprintf(&b, "command = %s\n", s)
	}
	if r.Type != "" {
		fmt.Fprintf(&b, "type = %s\n", basicString(r.Type))
	}
	s, err := literalString(r.Pattern)
	if err != nil {
		return fmt.Errorf("rule %q: pattern: %w", r.Name, err)
	}
	fmt.Fprintf(&b, "pattern = %s\n", s)
	if r.MaxMatches != nil {
		fmt.Fprintf(&b, "max_matches = %d\n", *r.MaxMatches)
	}
	s, err = messageString(r.Message)
	if err != nil {
		return fmt.Errorf("rule %q: message: %w", r.Name, err)
	}
	fmt.Fprintf(&b, "message = %s\n\n", s)
	_, err = io.WriteString(w, b.String())
	return err
}

// literalString quotes s as a TOML literal string, with single-quote
// delimiters when s has no single quote and triple-quote delimiters
// otherwise. Literal strings cannot hold every value, so it fails on the
// ones they cannot.
func literalString(s string) (string, error) {
	if strings.ContainsAny(s, "\n\r") {
		return "", fmt.Errorf("value contains a line break")
	}
	if !strings.Contains(s, "'") {
		return "'" + s + "'", nil
	}
	if strings.Contains(s, "'''") || strings.HasSuffix(s, "'") {
		return "", fmt.Errorf("value cannot be written as a TOML literal string")
	}
	return "'''" + s + "'''", nil
}

// basicString quotes s as a TOML basic string, escaping backslash and
// double quote. It expects s to have no control characters.
func basicString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// messageString quotes a message. Messages with a double quote use the
// literal form so they read as written; the rest are basic strings.
func messageString(s string) (string, error) {
	for _, c := range s {
		if c < 0x20 || c == 0x7f {
			return "", fmt.Errorf("value contains control character %s", strconv.QuoteRune(c))
		}
	}
	if strings.Contains(s, `"`) {
		if lit, err := literalString(s); err == nil {
			return lit, nil
		}
	}
	return basicString(s), nil
}
