package aec

import (
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// NudgeSettings tunes the nudge signals. Models maps a substring of a
// model id to the state-load threshold for that model.
type NudgeSettings struct {
	DirtyThreshold   int            `toml:"dirty_threshold"`
	ConfidenceStreak int            `toml:"confidence_streak"`
	TestFiles        int            `toml:"test_files"`
	TestAgent        string         `toml:"test_agent"`
	Models           map[string]int `toml:"models"`
}

// ThresholdFor returns the state-load threshold for a model id: the value
// of the longest Models key the id contains, ties going to the lexically
// smaller key, or DirtyThreshold when none matches.
func (s NudgeSettings) ThresholdFor(model string) int {
	best, threshold := "", s.DirtyThreshold
	if model == "" {
		return threshold
	}
	for _, key := range slices.Sorted(maps.Keys(s.Models)) {
		if strings.Contains(model, key) && len(key) > len(best) {
			best, threshold = key, s.Models[key]
		}
	}
	return threshold
}

// lowestThreshold is the smallest state-load threshold any model can get.
func (s NudgeSettings) lowestThreshold() int {
	lowest := s.DirtyThreshold
	for _, v := range s.Models {
		lowest = min(lowest, v)
	}
	return lowest
}

func (s NudgeSettings) validate() error {
	for _, v := range []struct {
		key string
		val int
	}{{"dirty_threshold", s.DirtyThreshold}, {"confidence_streak", s.ConfidenceStreak}, {"test_files", s.TestFiles}} {
		if v.val < 1 {
			return fmt.Errorf("nudge: %s must be at least 1", v.key)
		}
	}
	if s.TestAgent == "" {
		return fmt.Errorf("nudge: test_agent is empty")
	}
	for _, key := range slices.Sorted(maps.Keys(s.Models)) {
		if key == "" {
			return fmt.Errorf("nudge: model key is empty")
		}
		if s.Models[key] < 1 {
			return fmt.Errorf("nudge: models.%s must be at least 1", key)
		}
	}
	return nil
}

// LoadDefaultNudge decodes and validates the nudge settings embedded in
// the binary.
func LoadDefaultNudge() (NudgeSettings, error) {
	var f rulesFile
	if _, err := toml.Decode(string(defaultRulesTOML), &f); err != nil {
		return NudgeSettings{}, fmt.Errorf("decode rules: %w", err)
	}
	if err := f.Nudge.validate(); err != nil {
		return NudgeSettings{}, err
	}
	return f.Nudge, nil
}

// overlayNudge is the overlay's [nudge] table; every key is optional.
type overlayNudge struct {
	DirtyThreshold   *int           `toml:"dirty_threshold"`
	ConfidenceStreak *int           `toml:"confidence_streak"`
	TestFiles        *int           `toml:"test_files"`
	TestAgent        *string        `toml:"test_agent"`
	Models           map[string]int `toml:"models"`
}

// apply copies the keys the overlay gives onto s, which must own its
// Models map, and returns their names; model keys are "models.<key>".
func (o overlayNudge) apply(s *NudgeSettings) []string {
	var changed []string
	setInt := func(key string, dst *int, src *int) {
		if src != nil {
			*dst = *src
			changed = append(changed, key)
		}
	}
	setInt("dirty_threshold", &s.DirtyThreshold, o.DirtyThreshold)
	setInt("confidence_streak", &s.ConfidenceStreak, o.ConfidenceStreak)
	setInt("test_files", &s.TestFiles, o.TestFiles)
	if o.TestAgent != nil {
		s.TestAgent = *o.TestAgent
		changed = append(changed, "test_agent")
	}
	for _, key := range slices.Sorted(maps.Keys(o.Models)) {
		s.Models[key] = o.Models[key]
		changed = append(changed, "models."+key)
	}
	return changed
}

// mergeNudge applies the overlay's [nudge] table to the embedded defaults.
func mergeNudge(ov *overlayFile) (NudgeSettings, []string, error) {
	s, err := LoadDefaultNudge()
	if err != nil {
		return NudgeSettings{}, nil, err
	}
	s.Models = maps.Clone(s.Models)
	if ov == nil {
		return s, nil, nil
	}
	changed := ov.Nudge.apply(&s)
	if err := s.validate(); err != nil {
		return NudgeSettings{}, nil, err
	}
	return s, changed, nil
}

var bareKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// emitNudge writes the settings as [nudge] and [nudge.models] tables, model
// keys sorted, marking each key named in changed with "# overridden".
func emitNudge(w io.Writer, s NudgeSettings, changed []string) error {
	var b strings.Builder
	line := func(key, label, value string) {
		b.WriteString(label + " = " + value)
		if slices.Contains(changed, key) {
			b.WriteString(" # overridden")
		}
		b.WriteString("\n")
	}
	b.WriteString("# nudge settings\n[nudge]\n")
	line("dirty_threshold", "dirty_threshold", strconv.Itoa(s.DirtyThreshold))
	line("confidence_streak", "confidence_streak", strconv.Itoa(s.ConfidenceStreak))
	line("test_files", "test_files", strconv.Itoa(s.TestFiles))
	line("test_agent", "test_agent", basicString(s.TestAgent))
	b.WriteString("\n[nudge.models]\n")
	for _, key := range slices.Sorted(maps.Keys(s.Models)) {
		label := key
		if !bareKeyRe.MatchString(key) {
			label = basicString(key)
		}
		line("models."+key, label, strconv.Itoa(s.Models[key]))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// nudgeDiff lists the settings keys the overlay changed, with the default
// and effective values; a model key absent from the defaults shows "".
func nudgeDiff(defaults, s NudgeSettings, changed []string) []keyDiff {
	value := func(n NudgeSettings, key string) string {
		switch key {
		case "dirty_threshold":
			return strconv.Itoa(n.DirtyThreshold)
		case "confidence_streak":
			return strconv.Itoa(n.ConfidenceStreak)
		case "test_files":
			return strconv.Itoa(n.TestFiles)
		case "test_agent":
			return n.TestAgent
		}
		if v, ok := n.Models[strings.TrimPrefix(key, "models.")]; ok {
			return strconv.Itoa(v)
		}
		return ""
	}
	var out []keyDiff
	for _, key := range changed {
		out = append(out, keyDiff{key, value(defaults, key), value(s, key)})
	}
	return out
}
