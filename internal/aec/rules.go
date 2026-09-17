package aec

import (
	_ "embed"
	"fmt"
	"regexp"

	"github.com/BurntSushi/toml"
	"github.com/dlclark/regexp2"
)

//go:embed rules.toml
var defaultRulesTOML []byte

// Rule is one check. Files names the file types the rule runs on for
// Write/Edit content; Command is a PHP-style regex a Bash command must match
// for the rule to be consulted. A rule may set both.
type Rule struct {
	Name       string   `toml:"name"`
	Pattern    string   `toml:"pattern"`
	Message    string   `toml:"message"`
	Files      []string `toml:"files"`       // "php", ".blade.php", "migration.php", "*"
	Command    string   `toml:"command"`     // PHP-style regex the Bash command must match
	Type       string   `toml:"type"`        // "forbid" (default when empty) or "require"; command rules only
	MaxMatches *int     `toml:"max_matches"` // files only; nil means any match blocks

	re        *regexp2.Regexp // compiled Pattern
	commandRe *regexp2.Regexp // compiled Command, nil when Command is ""
}

// RuleSet is an ordered list of rules. Order is file order and is preserved
// so violations print in the same order the PHP hooks used.
type RuleSet []Rule

type rulesFile struct {
	Rules []Rule `toml:"rules"`
}

var nameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// LoadDefaults decodes the rules embedded in the binary, validates them and
// compiles every pattern.
func LoadDefaults() (RuleSet, error) {
	return parseRules(defaultRulesTOML)
}

func parseRules(data []byte) (RuleSet, error) {
	var f rulesFile
	if _, err := toml.Decode(string(data), &f); err != nil {
		return nil, fmt.Errorf("decode rules: %w", err)
	}
	seen := make(map[string]bool, len(f.Rules))
	for i := range f.Rules {
		r := &f.Rules[i]
		if err := validateRule(r); err != nil {
			return nil, err
		}
		if seen[r.Name] {
			return nil, fmt.Errorf("rule %q: duplicate name", r.Name)
		}
		seen[r.Name] = true
	}
	return RuleSet(f.Rules), nil
}

// validateRule checks one rule's fields and compiles its patterns in place.
// Every error names the rule.
func validateRule(r *Rule) error {
	if r.Name == "" {
		return fmt.Errorf("rule with pattern %q: name is empty", r.Pattern)
	}
	if !nameRe.MatchString(r.Name) {
		return fmt.Errorf("rule %q: name must be lower-case kebab-case", r.Name)
	}
	fail := func(format string, args ...any) error {
		return fmt.Errorf("rule %q: "+format, append([]any{r.Name}, args...)...)
	}
	if r.Pattern == "" {
		return fail("pattern is empty")
	}
	if r.Message == "" {
		return fail("message is empty")
	}
	if len(r.Files) == 0 && r.Command == "" {
		return fail("needs at least one of files or command")
	}
	switch r.Type {
	case "", "forbid":
	case "require":
		if r.Command == "" {
			return fail(`type "require" needs command`)
		}
	default:
		return fail(`type %q must be "forbid" or "require"`, r.Type)
	}
	if r.MaxMatches != nil && len(r.Files) == 0 {
		return fail("max_matches needs files")
	}
	re, err := CompilePattern(r.Pattern)
	if err != nil {
		return fail("%v", err)
	}
	r.re = re
	r.commandRe = nil
	if r.Command != "" {
		cre, err := CompilePattern(r.Command)
		if err != nil {
			return fail("command %v", err)
		}
		r.commandRe = cre
	}
	return nil
}
