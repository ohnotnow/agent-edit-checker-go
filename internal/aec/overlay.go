package aec

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"

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
	if _, err := toml.Decode(string(data), &ov); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &ov, nil
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
