package aec

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// configDir returns the directory aec keeps its files in. Tests replace it.
var configDir = defaultConfigDir

// defaultConfigDir is $XDG_CONFIG_HOME or ~/.config, except on Windows.
func defaultConfigDir() (string, error) {
	if runtime.GOOS == "windows" {
		return os.UserConfigDir()
	}
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config"), nil
}

// OverlayPath is where the user's overlay lives.
func OverlayPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "aec", "rules.toml"), nil
}

// overlayFile is the user's overlay: a list of default names to switch off,
// plus rule blocks that override a default's keys or add a new rule.
type overlayFile struct {
	Disabled []string      `toml:"disabled"`
	Rules    []overlayRule `toml:"rules"`

	// Undecoded lists keys in the file that nothing above reads, such as
	// a misspelt key or an enabled flag inside a rule block.
	Undecoded []string `toml:"-"`
}

// overlayRule has every key optional, so "not given" is distinguishable
// from "given as empty".
type overlayRule struct {
	Name       string    `toml:"name"`
	Pattern    *string   `toml:"pattern"`
	Message    *string   `toml:"message"`
	Files      *[]string `toml:"files"`
	Command    *string   `toml:"command"`
	Type       *string   `toml:"type"`
	MaxMatches *int      `toml:"max_matches"`
}

func (o overlayRule) complete() bool {
	return o.Pattern != nil && o.Message != nil && (o.Files != nil || o.Command != nil)
}

// apply copies the keys the overlay gives onto r and reports whether any
// key was given.
func (o overlayRule) apply(r *Rule) bool {
	changed := false
	if o.Pattern != nil {
		r.Pattern, changed = *o.Pattern, true
	}
	if o.Message != nil {
		r.Message, changed = *o.Message, true
	}
	if o.Files != nil {
		r.Files, changed = *o.Files, true
	}
	if o.Command != nil {
		r.Command, changed = *o.Command, true
	}
	if o.Type != nil {
		r.Type, changed = *o.Type, true
	}
	if o.MaxMatches != nil {
		r.MaxMatches, changed = o.MaxMatches, true
	}
	return changed
}

// Merged is the effective rule set after the overlay is applied.
type Merged struct {
	Rules []Rule   // defaults in file order, then user rules in overlay order
	Stale []string // overlay names that match no default and do not form a complete rule
}

// LoadOverlay decodes the overlay at path. A missing file is not an error:
// it returns nil, nil.
func LoadOverlay(path string) (*overlayFile, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ov overlayFile
	md, err := toml.Decode(string(data), &ov)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for _, k := range md.Undecoded() {
		ov.Undecoded = append(ov.Undecoded, k.String())
	}
	return &ov, nil
}

// starterExamples is the worked part of the starter overlay: valid TOML
// showing the three things an overlay can do. It is shipped commented out.
const starterExamples = `# 1. Switch a default off by name. aec rules list prints every rule and
#    aec rules show <name> prints one. This project has no Sentry, so an
#    agent throwing its own exceptions is fine here.
disabled = ["no-throw"]

# 2. Reword a default. Give the name and only the keys you want changed;
#    the rest of the rule stays as shipped.
[[rules]]
name = "one-test-at-a-time"
message = "Write one test, run it, then write the next."

# 3. Add a file rule of your own. It runs on every Write and Edit to the
#    listed file types and blocks the edit when pattern matches the new
#    content. The message is what the agent sees instead of the edit.
[[rules]]
name = "no-dd"
files = ["php"]
pattern = '/\bdd\(/'
message = "Don't use dd(). Use Log::debug() and check the log."

# 4. Add a command rule. It runs on every Bash call whose command matches
#    command, and blocks it when pattern also matches.
[[rules]]
name = "no-svn"
command = '/\bsvn\s/'
pattern = '/\bsvn\s+(checkout|co|commit|ci|update|up)\b/'
message = "It's 2026 - take a good look at yourself."
`

const starterHeader = `# aec overlay. The rules live in the aec binary; this file only lists
# what you change about them. Run aec rules list to see the result.
#
# A rule is a regex plus a message. File rules check the content an agent
# is about to write or edit; command rules check the Bash command it is
# about to run. When a rule matches, aec blocks the action and shows the
# agent the message.

`

// starterOverlay is the file written when a user has no overlay yet.
var starterOverlay = starterHeader + commentOut(starterExamples)

// commentOut prefixes every non-blank, non-comment line with "# ".
func commentOut(src string) string {
	lines := strings.Split(src, "\n")
	for i, l := range lines {
		if l != "" && !strings.HasPrefix(l, "#") {
			lines[i] = "# " + l
		}
	}
	return strings.Join(lines, "\n")
}

// ensureOverlay writes the starter overlay at path when no file exists
// there. It reports whether it wrote one.
func ensureOverlay(path string) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	if _, err := f.WriteString(starterOverlay); err != nil {
		return false, err
	}
	return true, nil
}

// Merge applies the overlay to the defaults by rule name.
func Merge(defaults []Rule, ov *overlayFile) (Merged, error) {
	rules := slices.Clone(defaults)
	if ov == nil {
		return Merged{Rules: rules}, nil
	}
	index := make(map[string]int, len(rules))
	for i, r := range rules {
		index[r.Name] = i
	}
	var stale []string
	seen := make(map[string]bool, len(ov.Rules))
	for i, o := range ov.Rules {
		if o.Name == "" {
			return Merged{}, fmt.Errorf("overlay rules[%d] has no name", i)
		}
		if seen[o.Name] {
			return Merged{}, fmt.Errorf("overlay rule %q: duplicate name", o.Name)
		}
		seen[o.Name] = true

		if j, ok := index[o.Name]; ok {
			r := rules[j]
			if !o.apply(&r) {
				continue
			}
			if err := validateRule(&r); err != nil {
				return Merged{}, fmt.Errorf("overlay %w", err)
			}
			r.Origin = OriginOverridden
			rules[j] = r
			continue
		}
		if !o.complete() {
			stale = append(stale, o.Name)
			continue
		}
		r := Rule{Name: o.Name, Enabled: true, Origin: OriginUser}
		o.apply(&r)
		if err := validateRule(&r); err != nil {
			return Merged{}, fmt.Errorf("overlay %w", err)
		}
		index[r.Name] = len(rules)
		rules = append(rules, r)
	}
	for _, name := range ov.Disabled {
		if j, ok := index[name]; ok {
			rules[j].Enabled = false
		} else if !slices.Contains(stale, name) {
			stale = append(stale, name)
		}
	}
	return Merged{Rules: rules, Stale: stale}, nil
}
