package aec

import (
	"fmt"
	"io"
	"strings"
)

// rulesList prints the effective rule set as TOML, one block per rule,
// each preceded by a comment naming its origin and disabled state.
func rulesList(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	if len(args) != 0 {
		return usageErr("rules list takes no arguments")
	}
	merged, _, err := loadForCLI(stderr)
	if err != nil {
		return err
	}
	for _, r := range merged.Rules {
		tag := r.Origin.String()
		if !r.Enabled {
			tag += ", disabled"
		}
		fmt.Fprintf(stdout, "# %s\n", tag)
		if err := emitRule(stdout, r); err != nil {
			return err
		}
	}
	return nil
}

// rulesShow prints one default rule as TOML, for pasting into the overlay.
func rulesShow(args []string, _ io.Reader, stdout, _ io.Writer) error {
	if len(args) != 1 {
		return usageErr("rules show needs exactly one rule name")
	}
	name := args[0]
	defaults, err := LoadDefaults()
	if err != nil {
		return err
	}
	for _, r := range defaults {
		if r.Name == name {
			return emitRule(stdout, r)
		}
	}
	return notFoundErr("no default rule named %q", name)
}

// rulesDiff prints, for every default the overlay touches, the changed
// keys with the default and override values, then the disabled list and
// the names of user rules.
func rulesDiff(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	if len(args) != 0 {
		return usageErr("rules diff takes no arguments")
	}
	merged, ov, err := loadForCLI(stderr)
	if err != nil {
		return err
	}
	defaults, err := LoadDefaults()
	if err != nil {
		return err
	}
	byName := make(map[string]Rule, len(defaults))
	for _, r := range defaults {
		byName[r.Name] = r
	}

	changed := false
	var users []string
	for _, r := range merged.Rules {
		switch r.Origin {
		case OriginUser:
			users = append(users, r.Name)
		case OriginOverridden:
			changed = true
			fmt.Fprintf(stdout, "%s\n", r.Name)
			for _, d := range ruleDiff(byName[r.Name], r) {
				fmt.Fprintf(stdout, "  %s\n    default:  %s\n    override: %s\n", d.key, d.before, d.after)
			}
			fmt.Fprintln(stdout)
		}
	}
	var disabled []string
	if ov != nil {
		for _, name := range ov.Disabled {
			if _, ok := byName[name]; ok {
				disabled = append(disabled, name)
			}
		}
	}
	if len(disabled) > 0 {
		changed = true
		fmt.Fprintf(stdout, "disabled: %s\n", strings.Join(disabled, ", "))
	}
	if len(users) > 0 {
		changed = true
		fmt.Fprintf(stdout, "user rules: %s\n", strings.Join(users, ", "))
	}
	if !changed {
		fmt.Fprintln(stdout, "no changes from default")
	}
	return nil
}

type keyDiff struct{ key, before, after string }

// ruleDiff lists the keys whose value differs between a and b, in the
// emitter's key order, with both values rendered for reading.
func ruleDiff(a, b Rule) []keyDiff {
	var out []keyDiff
	add := func(key, x, y string) {
		if x != y {
			out = append(out, keyDiff{key, x, y})
		}
	}
	add("files", strings.Join(a.Files, ", "), strings.Join(b.Files, ", "))
	add("command", a.Command, b.Command)
	add("type", ruleType(a), ruleType(b))
	add("pattern", a.Pattern, b.Pattern)
	add("max_matches", maxMatches(a), maxMatches(b))
	add("message", a.Message, b.Message)
	return out
}

func ruleType(r Rule) string {
	if r.Type == "" {
		return "forbid"
	}
	return r.Type
}

func maxMatches(r Rule) string {
	if r.MaxMatches == nil {
		return "unset"
	}
	return fmt.Sprint(*r.MaxMatches)
}
